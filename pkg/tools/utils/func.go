package utils

import (
	"cmp"
	"encoding/json"
	"github.com/OpenIMSDK/protocol/sdkws"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/table/unrelation"
	"github.com/tidwall/gjson"
	"slices"
)

// WaitGroupSetLimit 设置 waitGroup 初始化数量
func WaitGroupSetLimit(consLength int) int {
	var maxWorkers = config.Config.Push.MaxConcurrentWorkers
	if consLength < 3 {
		maxWorkers = 3
	} else if consLength < maxWorkers {
		maxWorkers = consLength
	}
	return maxWorkers
}

// VerifyRights 校验消息权限
func VerifyRights(ex string) (int, error) {
	// UserRights 用户权限对象
	type UserRights struct {
		AnchorAuth    string `json:"anchor_auth"`
		HighAuth      string `json:"high_auth"`
		OperationAuth string `json:"operation_auth"`
	}
	var userRights UserRights
	if ex != "" {
		err := json.Unmarshal([]byte(ex), &userRights)
		if err != nil {
			return 0, err
		}
	} else {
		userRights.AnchorAuth = "false"
		userRights.HighAuth = "false"
		userRights.OperationAuth = "false"
	}
	if userRights.HighAuth == "true" {
		return 3, nil
	}
	if userRights.OperationAuth == "true" {
		return 2, nil
	}
	if userRights.AnchorAuth == "true" {
		return 1, nil
	}
	return 0, nil
}

// VerifySendStatus 发送状态 sendStatus = 3 时，不执行发送逻辑
func VerifySendStatus(ex string) (int32, error) {
	// SendStatus 发送状态
	type SendStatus struct {
		SendStatus int32 `json:"sendStatus"`
	}
	var sendStatus SendStatus
	if ex != "" {
		err := json.Unmarshal([]byte(ex), &sendStatus)
		if err != nil {
			return 0, err
		}
	} else {
		sendStatus.SendStatus = 0
	}
	return sendStatus.SendStatus, nil
}

// InArray 查找某值是否在数组中
func InArray(v string, m []string) bool {
	for _, value := range m {
		if value == v {
			return true
		}
	}
	return false
}

// FilterMsg 过滤 已删除或者不应该显示的 消息
func FilterMsg(userAuth int, roleLevel int32, msg *unrelation.MsgInfoModel, userInfo *sdkws.UserInfo) bool {
	if msg.Msg.ContentType == 110 {
		//自定义消息
		var sdkMsg sdkws.MsgData
		sdkMsg.Content = []byte(msg.Msg.Content)
		customType, _ := CustomType(&sdkMsg)
		if customType == 1208 {
			//直播状态最后一条消息过滤
			return false
		} else if customType == 1101 {
			//领取红包消息
			customData, _ := CustomData(&sdkMsg, "")
			creatorId := gjson.Get(customData, "creatorId")
			rcreatorId := gjson.Get(customData, "rcreator")
			if userInfo.UserID != creatorId.String() && userInfo.UserID != rcreatorId.String() {
				//领取消息不是与我相关的消息过滤
				return false
			}
		}
		return true
	} else if msg.Revoke != nil {
		//该消息是撤回消息
		if userAuth == 0 && roleLevel == 20 {
			return false
		}
		return true
	} else if msg.Msg.ContentType == 1508 {
		//该消息是T人消息
		if userAuth == 0 && roleLevel == 20 {
			return false
		}
		return true
	}
	return true
}

// GetMsgSeqs 每次 获取 80 条消息的seq，最终只返回50条
func GetMsgSeqs(minSeq, end, pageMsgSize int64) []int64 {
	var seqs []int64
	var i int64
	for i = 0; i < pageMsgSize; i++ {
		tmp := end - i
		//设置检索消息最小Seq上限
		if tmp < minSeq {
			break
		}
		seqs = append(seqs, tmp)
	}
	slices.SortStableFunc(seqs, func(a, b int64) int {
		//降序
		return cmp.Compare(b, a)
	})
	return seqs
}
