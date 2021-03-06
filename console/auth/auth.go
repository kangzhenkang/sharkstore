package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/sessions"

	"util/log"
	"net/http"
	"strings"
	"time"
	"fmt"

	"console/common"
	"console/config"
)


var authConfig *AuthConfig

type UserInfo struct {
	UserId uint64 `json:"userId"`
	UserName string `json:"username"`
	Expire int64
}
type SsoReply struct {
	ReqCode int32 `json:"REQ_CODE"`
	ReqData *UserInfo `json:"REQ_DATA"`
	ReqFlag  bool `json:"REQ_FLAG"`
	ReqMsg	string `json:"REQ_MSG"`
}
type AuthConfig struct {
	cache map[string]*UserInfo
	loginConfig *config.LoginConfig
	client *http.Client
	cacheTime time.Duration
}

func initLoginConfig(cfg *config.Config)  *config.LoginConfig{
	return &config.LoginConfig{
		SsoLoginUrl: cfg.SsoLoginUrl,
		SsoLogoutUrl: cfg.SsoLogoutUrl,
		SsoCookieName: cfg.SsoCookieName,
		SsoDomainName: cfg.SsoDomainName,
		SsoExcludePath: cfg.SsoExcludePath,
		SsoVerifyUrl: cfg.SsoVerifyUrl,
		AppDomainName: cfg.AppDomainName,
		AppUrl: cfg.AppUrl,
		AppName: cfg.AppName,
		AppToken: cfg.AppToken,
	}
}

func Author(cfg *config.Config) gin.HandlerFunc {
	authConfig = &AuthConfig{
		loginConfig:initLoginConfig(cfg),
		cache: make(map[string]*UserInfo),
		client: &http.Client{
			Timeout: 3*time.Second,
		},
		cacheTime: 30*time.Minute,
	}

	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		// authority only when path is not being skipped
		if ok := isExclude(path); !ok {
			session := sessions.Default(c)
			userName, ok := session.Get("user_name").(string)
			if !ok {
				//从request获取cooke

				//根据tickey获取用户信息
				userInfo := getUserInfoFromCache("admin")
				if userInfo == nil {
					clientIP := c.ClientIP()
					log.Debug("get user info by tickey from sso service, ticket:[admin], ip:[%v], path:[%v]", clientIP, path)
					//去请求sso的ticket

					userInfo, _ =  getUserInfoFromRemote("admin", path, clientIP)

					saveUserInfoToCache("admin", userInfo)
				}
				userName = userInfo.UserName
				log.Debug("start to update session, user info: [%v]", userInfo)
				session.Set("user_name", userInfo.UserName)
				session.Save()
			}
			end := time.Now()
			latency := end.Sub(start)
			log.Debug("user[%v] login take time : [%v]", userName, latency)
		}
		// Process request
		c.Next()
	}
}

func getUserInfoFromCache(ticket string) *UserInfo {
	if authConfig.cache == nil {
		return nil
	}
	userInfo, found :=  authConfig.cache[ticket]
	if !found {
		log.Debug("can't found user info from cache by ticket[%v]", ticket)
		return nil
	}
	for _,c := range authConfig.cache {
		log.Debug("user cache is: [%v]", c)
	}
	if userInfo.Expire < time.Now().Unix() {
		delete(authConfig.cache, ticket)
		return nil
	}
	return userInfo
}

func saveUserInfoToCache(ticket string, userInfo *UserInfo) {
	expireTime := time.Now().Add(authConfig.cacheTime)
	log.Debug("set expire time is:[%v] ", expireTime)
	userInfo.Expire = expireTime.Unix()
	authConfig.cache[ticket] = userInfo
}



func getUserInfoFromRemote(ticket, uri, clintIp string) (*UserInfo, error){
	userInfo := &UserInfo{
		UserId:1,
		UserName:"admin",
		Expire:10*365*24*3600,
	}
	return userInfo,nil
}

func generateSsoSignMock(ticket string, ts int64) string{
	return common.Md5Sum(fmt.Sprintf("%s%d%s", "123456789", ts, ticket))
}

func generateSsoSign(ticket string, ts int64) string{
	return common.Md5Sum(fmt.Sprintf("%s%d%s", authConfig.loginConfig.AppToken, ts, ticket))
}

func isExclude(uri string) bool {
	if authConfig.loginConfig == nil || authConfig.loginConfig.SsoExcludePath == nil || len(authConfig.loginConfig.SsoExcludePath) == 0 {
		log.Debug("no exclude path cache")
		return false
	}
	for _, path := range authConfig.loginConfig.SsoExcludePath {
		if strings.HasPrefix(uri, path){
			return true
		}
	}
	return false
}
