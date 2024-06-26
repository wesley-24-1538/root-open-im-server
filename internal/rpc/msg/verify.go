// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package msg

import (
	"context"
	"github.com/openimsdk/open-im-server/v3/tools/live"
	"math/rand"
	"strconv"
	"time"

	"github.com/OpenIMSDK/protocol/constant"
	"github.com/OpenIMSDK/protocol/msg"
	"github.com/OpenIMSDK/protocol/sdkws"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/OpenIMSDK/tools/utils"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
)

var ExcludeContentType = []int{constant.HasReadReceipt}

type Validator interface {
	validate(pb *msg.SendMsgReq) (bool, int32, string)
}

type MessageRevoked struct {
	RevokerID                   string `json:"revokerID"`
	RevokerRole                 int32  `json:"revokerRole"`
	ClientMsgID                 string `json:"clientMsgID"`
	RevokerNickname             string `json:"revokerNickname"`
	RevokeTime                  int64  `json:"revokeTime"`
	SourceMessageSendTime       int64  `json:"sourceMessageSendTime"`
	SourceMessageSendID         string `json:"sourceMessageSendID"`
	SourceMessageSenderNickname string `json:"sourceMessageSenderNickname"`
	SessionType                 int32  `json:"sessionType"`
	Seq                         uint32 `json:"seq"`
}

func (m *msgServer) messageVerification(ctx context.Context, data *msg.SendMsgReq) error {
	//TODO 新增逻辑 当 ex 中得字段 sendStatus = 3 时，不执行发送逻辑
	sendStatus, _ := utils.VerifySendStatus(data.MsgData.Ex)
	if sendStatus == 3 {
		return errs.ErrNetwork.Wrap()
	}
	switch data.MsgData.SessionType {
	case constant.SingleChatType:
		if utils.IsContain(data.MsgData.SendID, config.Config.Manager.UserID) {
			return nil
		}
		if data.MsgData.ContentType <= constant.NotificationEnd &&
			data.MsgData.ContentType >= constant.NotificationBegin {
			return nil
		}
		if data.MsgData.MsgFrom == constant.SysMsgType {
			//TODO 新增逻辑，如果是后台管理员发送消息，不检测是否是黑名单、好友
			return nil
		} else {
			//TODO 新增逻辑，如果发送消息的用户为 主播、运营、高权限账号，不检测是否是黑名单、好友
			auth, _ := utils.VerifyRights(data.MsgData.Ex)
			if auth == 0 {
				black, err := m.friend.IsBlocked(ctx, data.MsgData.SendID, data.MsgData.RecvID)
				if err != nil {
					return err
				}
				if black {
					return errs.ErrBlockedByPeer.Wrap()
				}
				if *config.Config.MessageVerify.FriendVerify {
					friend, err := m.friend.IsFriend(ctx, data.MsgData.SendID, data.MsgData.RecvID)
					if err != nil {
						return err
					}
					if !friend {
						return errs.ErrNotPeersFriend.Wrap()
					}
					return nil
				}
			}
		}
		return nil
	case constant.SuperGroupChatType:
		groupInfo, err := m.Group.GetGroupInfoCache(ctx, data.MsgData.GroupID)
		if err != nil {
			return err
		}
		if groupInfo.Status == constant.GroupStatusDismissed &&
			data.MsgData.ContentType != constant.GroupDismissedNotification {
			return errs.ErrDismissedAlready.Wrap()
		}
		if groupInfo.GroupType == constant.SuperGroup {
			return nil
		}
		if utils.IsContain(data.MsgData.SendID, config.Config.Manager.UserID) {
			return nil
		}
		if data.MsgData.ContentType <= constant.NotificationEnd &&
			data.MsgData.ContentType >= constant.NotificationBegin {
			return nil
		}
		// memberIDs, err := m.GroupLocalCache.GetGroupMemberIDs(ctx, data.MsgData.GroupID)
		// if err != nil {
		// 	return err
		// }
		// if !utils.IsContain(data.MsgData.SendID, memberIDs) {
		// 	return errs.ErrNotInGroupYet.Wrap()
		// }

		groupMemberInfo, err := m.Group.GetGroupMemberCache(ctx, data.MsgData.GroupID, data.MsgData.SendID)
		if err != nil {
			if err == errs.ErrRecordNotFound {
				return errs.ErrNotInGroupYet.Wrap(err.Error())
			}
			return err
		}

		// 群主 和 管理员 不限制
		if groupMemberInfo.RoleLevel == constant.GroupOwner || groupMemberInfo.RoleLevel == constant.GroupAdmin {
			return nil
		}
		// 高权限：不检测禁言
		auth, _ := utils.VerifyRights(data.MsgData.Ex)
		if auth > 0 {
			return nil
		}
		// 群成员禁言时间
		if groupMemberInfo.MuteEndTime >= time.Now().UnixMilli() {
			return errs.ErrMutedInGroup.Wrap()
		}
		// 是否设置 群禁言
		if groupInfo.Status == constant.GroupStatusMuted {
			return errs.ErrMutedGroup.Wrap()
		}

		// 刷屏禁言 限制 | 普通群员
		// 只有普通群员 和 用户消息类型 才限制
		if groupMemberInfo.RoleLevel == constant.GroupOrdinaryUsers && data.MsgData.MsgFrom == constant.UserMsgType {
			// 获取redis连接
			redisClient := m.MsgDatabase.GetRedis()
			brushLimit := live.NewBrushLimit(redisClient)
			result, err := brushLimit.Check(data.MsgData.SendID)
			if result == false {
				return errs.GetSendMsgLimitErr(err.Error())
			}
		}
		return nil
	default:
		return nil
	}
}

func (m *msgServer) encapsulateMsgData(msg *sdkws.MsgData) {
	msg.ServerMsgID = GetMsgID(msg.SendID)
	if msg.SendTime == 0 {
		msg.SendTime = utils.GetCurrentTimestampByMill()
	}
	switch msg.ContentType {
	case constant.Text:
		fallthrough
	case constant.Picture:
		fallthrough
	case constant.Voice:
		fallthrough
	case constant.Video:
		fallthrough
	case constant.File:
		fallthrough
	case constant.AtText:
		fallthrough
	case constant.Merger:
		fallthrough
	case constant.Card:
		fallthrough
	case constant.Location:
		fallthrough
	case constant.Custom:
		fallthrough
	case constant.Quote:
		utils.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, true)
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, true)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderSync, true)
	case constant.Revoke:
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)
	case constant.HasReadReceipt:
		utils.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)
	case constant.Typing:
		utils.SetSwitchFromOptions(msg.Options, constant.IsHistory, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsPersistent, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderSync, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)
	}
}

func GetMsgID(sendID string) string {
	t := time.Now().Format("2006-01-02 15:04:05")
	return utils.Md5(t + "-" + sendID + "-" + strconv.Itoa(rand.Int()))
}

func (m *msgServer) modifyMessageByUserMessageReceiveOpt(
	ctx context.Context,
	userID, conversationID string,
	sessionType int,
	pb *msg.SendMsgReq,
) (bool, error) {
	opt, err := m.User.GetUserGlobalMsgRecvOpt(ctx, userID)
	if err != nil {
		return false, err
	}
	switch opt {
	case constant.ReceiveMessage:
	case constant.NotReceiveMessage:
		return false, nil
	case constant.ReceiveNotNotifyMessage:
		if pb.MsgData.Options == nil {
			pb.MsgData.Options = make(map[string]bool, 10)
		}
		utils.SetSwitchFromOptions(pb.MsgData.Options, constant.IsOfflinePush, false)
		return true, nil
	}
	// conversationID := utils.GetConversationIDBySessionType(conversationID, sessionType)
	singleOpt, err := m.Conversation.GetSingleConversationRecvMsgOpt(ctx, userID, conversationID)
	if errs.ErrRecordNotFound.Is(err) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	switch singleOpt {
	case constant.ReceiveMessage:
		return true, nil
	case constant.NotReceiveMessage:
		if utils.IsContainInt(int(pb.MsgData.ContentType), ExcludeContentType) {
			return true, nil
		}
		return false, nil
	case constant.ReceiveNotNotifyMessage:
		if pb.MsgData.Options == nil {
			pb.MsgData.Options = make(map[string]bool, 10)
		}
		utils.SetSwitchFromOptions(pb.MsgData.Options, constant.IsOfflinePush, false)
		return true, nil
	}
	return true, nil
}
