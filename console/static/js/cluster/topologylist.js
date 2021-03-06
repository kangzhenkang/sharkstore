
var app = angular.module('topologyInfo', []);
app.controller('rangeList', function($rootScope, $scope, $http, $timeout) {
	var dbName = $("#dbName").val();
	var tableName = $("#tableName").val();
	//集群id
	var clusterId = $('#clusterId').val();
    $.ajax({
        url:"/cluster/topology/getall",
        type:"post",
        async: false,
        contentType:"application/x-www-form-urlencoded; charset=UTF-8",
        dataType:"json",
        data:{
            "clusterId":clusterId
        },
        success: function(data){
            if(data.code === 0){
                $scope.rangeList = data.data;
            }else {
                swal("获取拓扑列表失败", data.msg, "error");
            }

        },
        error: function(res){
            swal("获取拓扑列表失败", res, "error");
        }
    });

    $scope.deleteRange = function(rng){
        $.ajax({
            url:"/range/delete",
            type:"post",
            contentType:"application/x-www-form-urlencoded; charset=UTF-8",
            dataType:"json",
            data:{
                "clusterId": clusterId,
                "rangeId": rng.id
            },
            success: function(data){
                if(data.code === 0){
                    swal("删除range成功!", data.msg, "success");
                }else {
                    swal("删除range失败", data.msg, "error");
                }
            },
            error: function(res){
                swal("删除range失败", res, "error");
            }
        });
    };

    $scope.viewRangeTopo = function (rng) {
        window.location.href = "/range/getRangeTopo?clusterId=" + clusterId + "&rangeId=" + rng.id;
    }
});