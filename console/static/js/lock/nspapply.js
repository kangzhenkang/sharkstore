$(function(){
    $("#applyNsp-form").validate({
        debug: true,
        focusInvalid: false,
        onkeyup: false,
        submitHandler: function(form){
            //执行提交
            applyLockNsp();
        },
        rules:{
            namespace:{
                required:true
            }
        },
        messages:{
            namespace:{
                required:"必填"
            }
        }
    });
    getCluster();

});

function getCluster() {
    $('#clusterSelect').empty();
    $.ajax({
        url:"/lock/cluster/get",
        type:"get",
        success: function(data){
            if(data.code === 0){
                if(data.data.length > 0){
                    var option;
                    for(var i = 0; i < data.data.length; i++){
                        option = $("<option>").val(data.data[i].id).text(data.data[i].name);
                    }
                    $('#clusterSelect').append(option);
                }
            }else {
                swal("获取集群列表失败", data.msg, "error");
            }
        },
        error: function(res){
            swal("获取集群列表失败", res, "error");
        }
    });

}

function applyLockNsp(){
    var clusterId = $("#clusterSelect").val();
	if(!hasText(clusterId)){
	    swal("请选择对应的集群信息，如果集群列表，请联系开发人员")
	    return
    }
    var namespace = $("#namespace").val();
    swal({
        title: "申请lock namespace?",
        type: "warning",
        showCancelButton: true,
        confirmButtonColor: "#DD6B55",
        confirmButtonText: "确认",
        closeOnConfirm: false
    },function(){
        //执行ajax提交
        $.ajax({
            url:"/lock/namespace/apply",
            type:"post",
            contentType:"application/x-www-form-urlencoded; charset=UTF-8",
            dataType:"json",
            data:{
                namespace:namespace,
                clusterId:clusterId
            },
            success: function(data){
                if(data.code === 0){
                    swal("申请成功!", data.msg, "success");
                    window.location.href = "/page/lock/viewNamespace";
                }else {
                    $('#submit').removeAttr("disabled");
                    swal("申请失败", data.msg, "error");
                }
            },
            error: function(res){
                $('#submit').removeAttr("disabled");
                swal("申请lock namespace失败", res, "error");
            }
        });
    });
}