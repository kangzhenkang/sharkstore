package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
	"strings"

	"model/pkg/kvrpcpb"
	"model/pkg/metapb"
	"util/bufalloc"
	"util/log"
	"master-server/server"
	"proxy/metric"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func httpReadQuery(r *http.Request) (*Query, error) {
	var err error

	bufferLen := int(r.ContentLength)
	if bufferLen <= 0 || bufferLen > 1024*1024*10 {
		bufferLen = 512
	}
	buffer := bufalloc.AllocBuffer(bufferLen)
	defer bufalloc.FreeBuffer(buffer)
	if _, err = buffer.ReadFrom(r.Body); err != nil {
		return nil, err
	}
	// defer r.Body.Close()

	var query *Query
	switch r.Header.Get("fbase-protocol-type") {
	case "protobuf":
		return nil, errors.New("protobuf is unsupported")
	case "json":
		fallthrough
	default:
		query = new(Query)
		log.Debug("query: %v", string(buffer.Bytes()))
		if err = json.Unmarshal(buffer.Bytes(), query); err != nil {
			return nil, err
		}
	}
	return query, nil
}

func httpSendReply(w http.ResponseWriter, reply interface{}) error {
	data, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	w.Header().Set("content-type", "application/json;charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

func (q *Query) commandFieldNameToLower() {
	if q.Command == nil {
		return
	}
	cmd := q.Command

	if len(cmd.Field) != 0 {
		field := cmd.Field
		for i, c := range field {
			field[i] = strings.ToLower(c)
		}
	}

	andLower := func (and *And) {
		if and.Field != nil {
			and.Field.Column = strings.ToLower(and.Field.Column)
		}
	}

	if cmd.Filter != nil {
		filter := cmd.Filter
		if len(filter.Order) != 0 {
			order := filter.Order
			for i, o := range order {
				order[i].By = strings.ToLower(o.By)
			}
		}
		if len(filter.And) != 0 {
			and := filter.And
			for _, a := range and {
				andLower(a)
			}
		}
	}

	if len(cmd.PKs) != 0 {
		pks := cmd.PKs
		for _, pk := range pks {
			for _, and := range pk {
				andLower(and)
			}
		}
	}
}

func (s *Server) handleKVCommand(w http.ResponseWriter, r *http.Request) {
	var (
		query *Query
		err   error
		reply *Reply
		commandType string
	)
	defer func() {
		if reply == nil {
			reply = &Reply{Code: errCommandRun, Message: ErrInternalError.Error()}
		}
		if commandType == "set" && reply.Code == 0 && reply.RowsAffected == 0 {
			log.Warn("handleKVCommand: should not enter here ")
		}
		if err := httpSendReply(w, reply); err != nil {
			log.Error("send http reply error(%v)", err)
			if r.Body != nil {
				r.Body.Close()
			}
		}
	}()

	if query, err = httpReadQuery(r); err != nil {
		log.Error("read query: %v", err)
		reply = &Reply{Code: errCommandParse, Message: ErrHttpCmdParse.Error()}
		if err == io.EOF || err == io.ErrUnexpectedEOF || err == io.ErrClosedPipe {
			if r.Body != nil {
				r.Body.Close()
			}
		}
		return
	}

	dbname := query.DatabaseName
	tname := query.TableName
	if len(dbname) == 0 {
		log.Error("args[dbName] wrong")
		reply = &Reply{Code: errCommandNoDb, Message: fmt.Errorf("dbname %v", ErrHttpCmdEmpty).Error()}
		return
	}
	if len(tname) == 0 {
		log.Error("args[tableName] wrong")
		reply = &Reply{Code: errCommandNoTable, Message: fmt.Errorf("tablename %v", ErrHttpCmdEmpty).Error()}
		return
	}
	if query.Command == nil {
		log.Error("args[Command] wrong")
		reply = &Reply{Code: errCommandEmpty, Message: ErrHttpCmdEmpty.Error()}
		return
	}

	t := s.proxy.router.FindTable(dbname, tname)
	if t == nil {
		log.Error("table %s.%s doesn.t exist", dbname, tname)
		reply = &Reply{Code: errCommandNoTable, Message: ErrNotExistTable.Error()}
		return
	}

	start := time.Now()
    	var slowLogThreshold int
	query.commandFieldNameToLower()
	commandType = query.Command.Type
	switch query.Command.Type {
	case "get":
		slowLogThreshold = s.proxy.config.SelectSlowLog
		reply, err = query.getCommand(s.proxy, t)
		if err != nil {
			log.Error("getcommand error: %v", err)
			reply = &Reply{Code: errCommandRun, Message: fmt.Errorf("%v: %v", ErrHttpCmdRun, err).Error()}
		}
	case "set":
		slowLogThreshold = s.proxy.config.InsertSlowLog
		reply, err = query.setCommand(s.proxy, t)
		if err != nil {
			log.Error("setcommand error: %v", err)
			reply = &Reply{Code: errCommandRun, Message: fmt.Errorf("%v: %v", ErrHttpCmdRun, err).Error()}
		}
	case "del":
		slowLogThreshold = s.proxy.config.SelectSlowLog
		reply, err = query.delCommand(s.proxy, t)
		if err != nil {
			log.Error("delcommand error: %v", err)
			reply = &Reply{Code: errCommandRun, Message: fmt.Errorf("%v: %v", ErrHttpCmdRun, err).Error()}
		}
	default:
		log.Error("unknown command")
		reply = &Reply{Code: errCommandUnknown, Message: ErrHttpCmdUnknown.Error()}
	}

	delay := time.Since(start)
	if reply.Code == 0 {
		metric.GsMetric.ProxyApiMetric(query.Command.Type, true, delay)
	} else {
		metric.GsMetric.ProxyApiMetric(query.Command.Type, false, delay)
	}
	if delay > time.Duration(slowLogThreshold) * time.Millisecond {
		cmd, _ := json.Marshal(query)
		metric.GsMetric.SlowLogMetric(string(cmd), delay)
		log.Debug("[kvcommand slow log %v %v ", delay.String(), string(cmd))
	}
}

func (query *Query) getCommand(proxy *Proxy, t *Table) (*Reply, error) {
	log.Debug("get command ........... %v", query)
	log.Debug("get command: %v", query.Command)
	// 解析选择列
	columns := query.parseColumnNames()
	fieldList := make([]*kvrpcpb.SelectField, 0, len(columns))
	for _, c := range columns {
		col := t.FindColumn(c)
		if col == nil {
			return nil, fmt.Errorf("invalid column(%s)", c)
		}
		fieldList = append(fieldList, &kvrpcpb.SelectField{
			Typ:    kvrpcpb.SelectField_Column,
			Column: col,
		})
	}

	if len(query.Command.PKs) == 0 {
		order := query.parseOrder()
		log.Debug("getcommand order: %v", order)
		var matchs []Match = nil
		var err error
		if query.Command.Filter != nil {
			// 解析where条件
			matchs, err = query.parseMatchs(query.Command.Filter.And)
			if err != nil {
				log.Error("[get] handle parse where error: %v", err)
				return nil, err
			}
		}
		// 向dataserver查询
		//filter := &Filter{columns: columns, matchs: matchs}

		limit := query.parseLimit()
		log.Debug("getcommand limit: %v", limit)

		scope := query.parseScope()
		rowss, err := proxy.doSelect(t, fieldList, matchs, limit, scope)

		if err != nil {
			log.Error("getcommand doselect error: %v", err)
			return nil, err
		}
		return formatReply(t.columns, rowss, order, columns), nil
	} else {
		var allRows [][]*Row
		if len(query.Command.PKs) > 1 {
			var tasks []*SelectTask
			// TODO
			for _, pk := range query.Command.PKs {
				matchs, err := query.parseMatchs(pk)
				//filter := &Filter{columns: columns, matchs: matchs}
				if err != nil {
					log.Error("[get] handle parse where error: %v", err)
					return nil, err
				}
				task := GetSelectTask()
				task.init(proxy, t, fieldList, matchs)
				err = proxy.Submit(task)
				if err != nil {
					log.Error("submit insert task failed, err[%v]", err)
					return nil, err
				}
				tasks = append(tasks, task)
			}
			for _, task := range tasks {
				err := task.Wait()
				if err != nil {
					log.Error("select task do failed, err[%v]", err)
					PutSelectTask(task)
					return nil, err
				}
				rowss := task.rest.rows
				if rowss != nil {
					allRows = append(allRows, rowss...)
				}
				PutSelectTask(task)
			}
		} else {
			matchs, err := query.parseMatchs(query.Command.PKs[0])
			if err != nil {
				log.Error("[get] handle parse where error: %v", err)
				return nil, err
			}
			allRows, err = proxy.doSelect(t, fieldList, matchs, nil, nil)
			if err != nil {
				log.Error("select do failed, err[%v]", err)
				return nil, err
			}
		}


		return formatReply(t.columns, allRows, nil, columns), nil
	}
}

func formatReply(columnMap map[string]*metapb.Column, rowss [][]*Row, order []*Order, columns []string) *Reply {
	rowset := make([][]interface{}, 0)
	for _, rows := range rowss {
		for _, row := range rows {
			row_ := make([]interface{}, 0)
			for _, f := range row.fields {
				if f.value == nil {
					row_ = append(row_, nil)
					continue
				}
				switch columnMap[f.col].GetDataType() {
				case metapb.DataType_Tinyint:
					fallthrough
				case metapb.DataType_Smallint:
					fallthrough
				case metapb.DataType_Int:
					fallthrough
				case metapb.DataType_BigInt:
					if columnMap[f.col].GetUnsigned() {
						if i, ok := f.value.(uint64); ok {
							row_ = append(row_, i)
						} else {
							log.Error("column %v is not uint64", f.col)
							return nil
						}
					} else {
						if i, ok := f.value.(int64); ok {
							row_ = append(row_, i)
						} else {
							log.Error("column %v is not int64", f.col)
							return nil
						}
					}
				case metapb.DataType_Float:
					fallthrough
				case metapb.DataType_Double:
					if ff, ok := f.value.(float64); ok {
						row_ = append(row_, ff)
					} else {
						log.Error("column %v is not float64", f.col)
						return nil
					}
				case metapb.DataType_Date:
					fallthrough
				case metapb.DataType_TimeStamp:
					fallthrough
				case metapb.DataType_Varchar:
					if str, ok := f.value.([]byte); ok {
						row_ = append(row_, string(str))
					} else {
						log.Error("column %v is not []byte", f.col)
						return nil
					}
				case metapb.DataType_Binary:
					row_ = append(row_, f.value)
				}
			}
			log.Debug("row: %v", row_)
			rowset = append(rowset, row_)
		}
	}

	if order != nil {
		for _, o := range order {
			for n, c := range columns {
				if c == o.By {
					// sort
					sorter := &rowsetSorter{
						rowset:          rowset,
						orderByFieldNum: n,
						column:          columnMap[c],
					}
					sort.Sort(sorter)
				}
			}
			break // TODO just loop once now
		}
	}

	return &Reply{
		Code:   0,
		Values: rowset,
	}
}

type rowsetSorter struct {
	rowset          [][]interface{}
	orderByFieldNum int
	column          *metapb.Column
}

func (s *rowsetSorter) Len() int {
	return len(s.rowset)
}

func (s *rowsetSorter) Less(i, j int) bool {
	switch s.column.GetDataType() {
	case metapb.DataType_Tinyint:
		fallthrough
	case metapb.DataType_Smallint:
		fallthrough
	case metapb.DataType_Int:
		fallthrough
	case metapb.DataType_BigInt:
		if s.column.GetUnsigned() {
			return uint64(s.rowset[i][s.orderByFieldNum].(uint64)) < uint64(s.rowset[j][s.orderByFieldNum].(uint64))
		} else {
			return int64(s.rowset[i][s.orderByFieldNum].(int64)) < int64(s.rowset[j][s.orderByFieldNum].(int64))
		}
	case metapb.DataType_Float:
		fallthrough
	case metapb.DataType_Double:
		return float64(s.rowset[i][s.orderByFieldNum].(float64)) < float64(s.rowset[j][s.orderByFieldNum].(float64))
	case metapb.DataType_Date:
		fallthrough
	case metapb.DataType_TimeStamp:
		fallthrough
	case metapb.DataType_Varchar:
		//return string(s.rowset[i][s.orderByFieldNum].([]byte)) < string(s.rowset[j][s.orderByFieldNum].([]byte))
		return bytes.Compare([]byte(s.rowset[i][s.orderByFieldNum].(string)), []byte(s.rowset[j][s.orderByFieldNum].(string))) == -1
	case metapb.DataType_Binary:
		return bytes.Compare([]byte(s.rowset[i][s.orderByFieldNum].([]byte)), []byte(s.rowset[j][s.orderByFieldNum].([]byte))) == -1
	default:
		log.Error("rowset sorter: invalid datatype")
		return false
	}
}

func (s *rowsetSorter) Swap(i, j int) {
	s.rowset[i], s.rowset[j] = s.rowset[j], s.rowset[i]
}

func (query *Query) setCommand(proxy *Proxy, t *Table) (*Reply, error) {
	log.Debug("set command ........... %v", query)
	db := t.DbName()
	tableName := t.Name()
	// 解析选择列
	cols := query.parseColumnNames()

	// 按照表的每个列查找对应列值位置
	colMap, t, err := proxy.matchInsertValues(t, cols)
	if err != nil {
		log.Error("[insert] table %s.%s match column values error(%v)", db, tableName, err)
		return nil, err
	}

	// 检查是否缺少某列
	// TODO：支持默认值
	/*if err := proxy.checkMissingColumn(t, colMap); err != nil {
		log.Error("[insert] table %s.%s missing column(%v)", db, tableName, err)
		return nil, err
	}*/

	buffer := bufalloc.AllocBuffer(512)
	defer bufalloc.FreeBuffer(buffer)
	rows, err := query.parseRowValues(buffer)
	// rows, err := query.parseRowValues(t)
	if err != nil {
		log.Error("parse row values error: %v", err)
		return nil, err
	}

	affected, duplicateKey, err := proxy.insertRows(t, colMap, rows)
	if err != nil {
		log.Error("insert error %s- %s:%s", db, tableName, err.Error())
		return nil, err
	}
	if len(duplicateKey) > 0 {
		return nil, fmt.Errorf("duplicate key: %v", duplicateKey)
	}else if affected != uint64(len(rows)){
		log.Error("insert error table[%s:%s],request num:%d,inserted num:%d", db, tableName, len(rows),affected)
		return nil,ErrAffectRows
	}
	return &Reply{
		Code:         0,
		RowsAffected: affected,
	}, nil
}

func (query *Query) delCommand(proxy *Proxy, t *Table) (*Reply, error) {
	// 解析选择列
	//columns := query.parseColumnNames()
	var matchs []Match = nil
	var err error
	if query.Command.Filter != nil {
		// 解析where条件
		matchs, err = query.parseMatchs(query.Command.Filter.And)
		if err != nil {
			log.Error("[get] handle parse where error: %v", err)
			return nil, err
		}
	}

	// 向dataserver查询
	affectedRows, err := proxy.doDelete(t, matchs)
	if err != nil {
		return nil, err
	}
	return &Reply{
		Code:         0,
		RowsAffected: affectedRows,
	}, nil
}

func (s *Server) handleTableInfo(w http.ResponseWriter, r *http.Request) {
	dbname := r.FormValue("dbname")
	tname := r.FormValue("tablename")

	resp := new(Response)
	defer httpSendReply(w, resp)

	t := s.proxy.router.FindTable(dbname, tname)
	if t == nil {
		resp.Code = 1
		resp.Message = ErrNotExistTable.Error()
		return
	}

	type ColumnInfo struct {
		ColumnName string `json:"column_name"`
		DataType   string `json:"data_type"`
	}
	type RangeInfo struct {
		RangeId  uint64 `json:"range_id"`
		StartKey []byte `json:"start_key"`
		EndKey   []byte `json:"end_key"`
	}
	type tableInfo struct {
		Primarys []string      `json:"primarys"`
		Columns  []*ColumnInfo `json:"columns"`
		Ranges   []*RangeInfo  `json:"routes"`
	}
	tInfo := new(tableInfo)
	tInfo.Primarys = t.PKS()
	tInfo.Columns = func() []*ColumnInfo {
		var colInfos []*ColumnInfo
		for _, col := range t.GetAllColumns() {
			colInfos = append(colInfos, &ColumnInfo{
				ColumnName: col.GetName(),
				DataType:   metapb.DataType_name[int32(col.GetDataType())],
			})
		}
		return colInfos
	}()
	tInfo.Ranges = func() []*RangeInfo {
		var rngInfos []*RangeInfo
		for _, rng := range t.AllRoutes() {
			rngInfos = append(rngInfos, &RangeInfo{
				RangeId:  rng.Region.Id,
				StartKey: rng.StartKey,
				EndKey:   rng.EndKey,
			})
		}
		log.Debug("table range info: %v", rngInfos)
		return rngInfos
	}()
	resp.Data = tInfo
}


func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	var (
		query *CreateDatabase
		err   error
		reply *Response
	)

	defer func() {
		if reply == nil {
			reply = new(Response)
		}
		if err := httpSendReply(w, reply); err != nil {
			log.Error("send http reply error(%v)", err)
			if r.Body != nil {
				r.Body.Close()
			}
		}
	}()
	if query, err = httpReadCreateDatabase(r); err != nil {
		log.Error("read query: %v", err)
		if err == io.EOF || err == io.ErrUnexpectedEOF || err == io.ErrClosedPipe {
			if r.Body != nil {
				r.Body.Close()
			}
		}
		reply = &Response{Code: errCommandParse, Message: ErrHttpCmdParse.Error()}
		return
	}

	dbname := query.DatabaseName
	d := s.proxy.router.FindDB(dbname)
	if d != nil {
		log.Error("db %s already exist", dbname)
		return
	}

	err = s.proxy.msCli.CreateDatabase(dbname)
	if err != nil {
		reply = &Response{Code: errCreateDatabase, Message: err.Error()}
		return
	}
	return
}

func (s *Server) handleLockDebug(w http.ResponseWriter, r *http.Request) {
	dbName := r.FormValue("dbName")
	tableName := r.FormValue("tableName")
	lockName := r.FormValue("lockName")
	switch r.FormValue("type") {
	case "lock":
		userName := r.FormValue("userName")
		userCondition := []byte(r.FormValue("userCondition"))
		uuid := r.FormValue("uuid")
		deleteTime, err := strconv.ParseInt(r.FormValue("deleteTime"), 10, 64)
		if err != nil {
			w.Write([]byte("deleteTime: "+err.Error()))
			return
		}
		resp, err := s.proxy.Lock(dbName, tableName, lockName, userCondition, uuid, deleteTime, userName)
		if err != nil {
			w.Write([]byte("lock: "+err.Error()))
			return
		}
		reply, err := json.Marshal(resp)
		if err != nil {
			w.Write([]byte("lock reply marshal: "+err.Error()))
			return
		}
		w.Write(reply)
	case "lockupdate":
		uuid := r.FormValue("uuid")
		resp, err := s.proxy.LockUpdate(dbName, tableName, lockName, uuid, []byte(""))
		if err != nil {
			w.Write([]byte("lockupdate: "+err.Error()))
			return
		}
		reply, err := json.Marshal(resp)
		if err != nil {
			w.Write([]byte("lockupdate reply marshal: "+err.Error()))
			return
		}
		w.Write(reply)
	case "unlock":
		uuid := r.FormValue("uuid")
		userName := r.FormValue("userName")
		resp, err := s.proxy.Unlock(dbName, tableName, lockName, uuid, userName)
		if err != nil {
			w.Write([]byte("unlock reply marshal: "+err.Error()))
			return
		}
		reply, err := json.Marshal(resp)
		if err != nil {
			w.Write([]byte("unlock reply marshal: "+err.Error()))
			return
		}
		w.Write(reply)
	case "unlockforce":
		userName := r.FormValue("userName")
		resp, err := s.proxy.UnlockForce(dbName, tableName, lockName, userName)
		if err != nil {
			w.Write([]byte("unlockforce reply marshal: "+err.Error()))
			return
		}
		reply, err := json.Marshal(resp)
		if err != nil {
			w.Write([]byte("unlockforce reply marshal: "+err.Error()))
			return
		}
		w.Write(reply)
	default:
		w.Write([]byte("unknown type"))
	}
}

func (s *Server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	var (
		query *CreateTable
		err   error
		reply *Response
	)

	defer func() {
		if reply == nil {
			reply = new(Response)
		}
		if err := httpSendReply(w, reply); err != nil {
			log.Error("send http reply error(%v)", err)
			if r.Body != nil {
				r.Body.Close()
			}
		}
	}()
	if query, err = httpReadCreateTable(r); err != nil {
		log.Error("read createtable request: %v", err)
		if err == io.EOF || err == io.ErrUnexpectedEOF || err == io.ErrClosedPipe {
			if r.Body != nil {
				r.Body.Close()
			}
		}
		reply = &Response{Code: errCommandParse, Message: ErrHttpCmdParse.Error()}
		return
	}

	dbname := query.DatabaseName
	tablename := query.TableName
	columns := func() *server.TableProperty {
		properties := new(server.TableProperty)
		var cols []*metapb.Column
		for _, col := range query.Columns {
			cols = append(cols, &metapb.Column{
				Name: col.Name,
				DataType: func() metapb.DataType {
					var datatype string
					switch strings.ToLower(col.DataType) {
					case "tinyint":
						datatype = "Tinyint"
					case "smallint":
						datatype = "Smallint"
					case "int":
						datatype = "Int"
					case "bigint":
						datatype = "BigInt"
					case "float":
						datatype = "Float"
					case "double":
						datatype = "Double"
					case "varchar":
						datatype = "Varchar"
					case "binary":
						datatype = "Binary"
					case "date":
						datatype = "Date"
					case "timestamp":
						datatype = "TimeStamp"
					}
					if typ, ok := metapb.DataType_value[datatype]; ok {
						return metapb.DataType(typ)
					} else {
						return 0 // invalid
					}
				}(),
				PrimaryKey: func() uint64 {
					if col.PrimaryKey {
						return 1
					} else {
						return 0
					}
				}(),
				Unsigned: col.Unsigned,
			})
		}
		properties.Columns = cols
		return properties
	}()

	properties, err := json.Marshal(columns)
	if err != nil {
		reply = &Response{Code: errCreateTable, Message: err.Error()}
		return
	}
	err = s.proxy.msCli.CreateTable(dbname, tablename, string(properties))
	if err != nil {
		reply = &Response{Code: errCreateTable, Message: err.Error()}
		return
	}
	return
}

func httpReadCreateDatabase(r *http.Request) (*CreateDatabase, error) {
	var err error

	bufferLen := int(r.ContentLength)
	if bufferLen <= 0 || bufferLen > 1024*1024*10 {
		bufferLen = 512
	}
	buffer := bufalloc.AllocBuffer(bufferLen)
	defer bufalloc.FreeBuffer(buffer)
	if _, err = buffer.ReadFrom(r.Body); err != nil {
		return nil, err
	}

	var query *CreateDatabase
	switch r.Header.Get("fbase-protocol-type") {
	case "protobuf":
		return nil, errors.New("protobuf is unsupported")
	case "json":
		fallthrough
	default:
		query = new(CreateDatabase)
		log.Debug("query: %v", string(buffer.Bytes()))
		if err = json.Unmarshal(buffer.Bytes(), query); err != nil {
			return nil, err
		}
	}
	return query, nil
}

func httpReadCreateTable(r *http.Request) (*CreateTable, error) {
	var err error

	bufferLen := int(r.ContentLength)
	if bufferLen <= 0 || bufferLen > 1024*1024*10 {
		bufferLen = 512
	}
	buffer := bufalloc.AllocBuffer(bufferLen)
	defer bufalloc.FreeBuffer(buffer)
	if _, err = buffer.ReadFrom(r.Body); err != nil {
		return nil, err
	}
	// defer r.Body.Close()

	var query *CreateTable
	switch r.Header.Get("fbase-protocol-type") {
	case "protobuf":
		return nil, errors.New("protobuf is unsupported")
	case "json":
		fallthrough
	default:
		query = new(CreateTable)
		log.Debug("query: %v", string(buffer.Bytes()))
		if err = json.Unmarshal(buffer.Bytes(), query); err != nil {
			return nil, err
		}
	}
	return query, nil
}