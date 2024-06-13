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
	"fmt"
	"github.com/goccy/go-json"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"

	"github.com/OpenIMSDK/protocol/constant"
	"github.com/OpenIMSDK/protocol/msg"
	"github.com/OpenIMSDK/protocol/sdkws"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/OpenIMSDK/tools/log"
	"github.com/OpenIMSDK/tools/mcontext"
	"github.com/OpenIMSDK/tools/utils"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	unrelationtb "github.com/openimsdk/open-im-server/v3/pkg/common/db/table/unrelation"
)

func (m *msgServer) RevokeMsg(ctx context.Context, req *msg.RevokeMsgReq) (*msg.RevokeMsgResp, error) {
	defer log.ZDebug(ctx, "RevokeMsg return line")
	if req.UserID == "" {
		return nil, errs.ErrArgs.Wrap("user_id is empty")
	}
	if req.ConversationID == "" {
		return nil, errs.ErrArgs.Wrap("conversation_id is empty")
	}
	if req.Seq < 0 {
		return nil, errs.ErrArgs.Wrap("seq is invalid")
	}
	if err := authverify.CheckAccessV3(ctx, req.UserID); err != nil {
		return nil, err
	}
	user, err := m.User.GetUserInfo(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	groupMemberCache := &sdkws.GroupMemberFullInfo{}
	_, _, msgs, err := m.MsgDatabase.GetMsgBySeqs(ctx, req.UserID, req.ConversationID, []int64{req.Seq}, user, groupMemberCache)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 || msgs[0] == nil {
		return nil, errs.ErrRecordNotFound.Wrap("msg not found")
	}
	adminAuth := authverify.IsAppManagerUid(ctx)
	if msgs[0].ContentType == constant.MsgRevokeNotification {
		return nil, errs.ErrMsgAlreadyRevoke.Wrap("msg already revoke")
	} else if msgs[0].SessionType == constant.SingleChatType && !adminAuth {
		//TODO 私信不可撤回消息
		return nil, errs.ErrNoPermission.Wrap("no permission")
	}
	data, _ := json.Marshal(msgs[0])
	log.ZInfo(ctx, "GetMsgBySeqs", "conversationID", req.ConversationID, "seq", req.Seq, "msg", string(data), "adminAuth", adminAuth)
	var role int32
	//登录用户的权限
	userAuth, _ := utils.VerifyRights(req.Ex)
	//消息用户的权限
	msgsEx := msgs[0].Ex
	msgUserAuth, _ := utils.VerifyRights(msgsEx)
	if msgUserAuth == 3 && req.UserID != msgs[0].SendID && !adminAuth {
		//撤回的消息用户是高权限，且不是发消息的用户
		return nil, errs.ErrNoPermission.Wrap("no permission")
	} else if userAuth == 3 {
		//登录用户是高权限
		role = constant.AppAdmin
	}
	if role == 0 && !adminAuth {
		//TODO 原逻辑
		switch msgs[0].SessionType {
		case constant.SingleChatType:
			if err := authverify.CheckAccessV3(ctx, msgs[0].SendID); err != nil {
				return nil, err
			}
			role = user.AppMangerLevel
		case constant.SuperGroupChatType:
			members, err := m.Group.GetGroupMemberInfoMap(
				ctx,
				msgs[0].GroupID,
				utils.Distinct([]string{req.UserID, msgs[0].SendID}),
				false,
			)
			if err != nil {
				return nil, err
			}
			if req.UserID != msgs[0].SendID {
				switch members[req.UserID].RoleLevel {
				case constant.GroupOwner:
				case constant.GroupAdmin:
					if members[msgs[0].SendID].RoleLevel != constant.GroupOrdinaryUsers {
						return nil, errs.ErrNoPermission.Wrap("no permission")
					}
				default:
					return nil, errs.ErrNoPermission.Wrap("no permission")
				}
			}
			if member := members[req.UserID]; member != nil {
				role = member.RoleLevel
			}
		default:
			return nil, errs.ErrInternalServer.Wrap("msg sessionType not supported")
		}
	}
	if (role == constant.GroupOrdinaryUsers || role == constant.IMOrdinaryUser) && !adminAuth {
		return nil, errs.ErrNoPermission.Wrap("no permission")
	}
	now := time.Now().UnixMilli()
	err = m.MsgDatabase.RevokeMsg(ctx, req.ConversationID, req.Seq, &unrelationtb.RevokeModel{
		Role:     role,
		UserID:   req.UserID,
		Nickname: user.Nickname,
		Time:     now,
	})
	if err != nil {
		return nil, err
	}

	// 推送至es队列
	_id := fmt.Sprintf("%s%d", msgs[0].ServerMsgID, req.Seq)
	_ = m.MsgDatabase.RevokeMsgToEsMQ(ctx, _id)

	revokerUserID := mcontext.GetOpUserID(ctx)
	tips := sdkws.RevokeMsgTips{
		RevokerUserID:  revokerUserID,
		ClientMsgID:    msgs[0].ClientMsgID,
		RevokeTime:     now,
		Seq:            req.Seq,
		SesstionType:   msgs[0].SessionType,
		ConversationID: req.ConversationID,
		IsAdminRevoke:  utils.Contain(revokerUserID, config.Config.Manager.UserID...),
	}
	var recvID string
	if msgs[0].SessionType == constant.SuperGroupChatType {
		recvID = msgs[0].GroupID
	} else {
		recvID = msgs[0].RecvID
	}
	if err := m.notificationSender.NotificationWithSessionType(ctx, req.UserID, recvID, constant.MsgRevokeNotification, msgs[0].SessionType, &tips); err != nil {
		return nil, err
	}
	if err = CallbackAfterRevokeMsg(ctx, req); err != nil {
		return nil, err
	}
	//_ = m.MsgDatabase.SetRevokeConversationIdExpire(ctx, req.ConversationID, msgs[0].ClientMsgID)
	return &msg.RevokeMsgResp{}, nil
}
