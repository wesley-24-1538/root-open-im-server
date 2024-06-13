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
	"encoding/json"
	"github.com/OpenIMSDK/protocol/constant"
	pbconversation "github.com/OpenIMSDK/protocol/conversation"
	pbmsg "github.com/OpenIMSDK/protocol/msg"
	"github.com/OpenIMSDK/protocol/sdkws"
	"github.com/OpenIMSDK/protocol/wrapperspb"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/OpenIMSDK/tools/log"
	"github.com/OpenIMSDK/tools/mcontext"
	"github.com/OpenIMSDK/tools/utils"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/tools/live"
	"strconv"
	"strings"
	"time"
)

func (m *msgServer) SendMsg(ctx context.Context, req *pbmsg.SendMsgReq) (resp *pbmsg.SendMsgResp, error error) {
	resp = &pbmsg.SendMsgResp{}
	if req.MsgData != nil {
		flag := isMessageHasReadEnabled(req.MsgData)
		if !flag {
			return nil, errs.ErrMessageHasReadDisable.Wrap()
		}
		content := strings.TrimSpace(string(req.MsgData.Content))
		if content == "" {
			return nil, errs.ErrArgs.Wrap("请输入发送内容")
		}
		// 权限验证，是否敏感词过滤
		auth, _ := utils.VerifyRights(req.MsgData.Ex)
		if auth == 0 && !authverify.IsAppManagerUid(ctx) {
			contentType := req.MsgData.ContentType
			// 敏感词 | 只验证文本
			contentTypes2 := make([]int32, 0)
			contentTypes2 = append(contentTypes2, 101, 106, 114)
			if utils.IsContainInt32(contentType, contentTypes2) {
				type Content1 struct {
					Content string `json:"content"`
				}
				type Content2 struct {
					Content string `json:"text"`
				}

				var content string
				if contentType == 101 {
					var tmp Content1
					_ = json.Unmarshal(req.MsgData.Content, &tmp)
					content = tmp.Content
				} else {
					var tmp Content2
					_ = json.Unmarshal(req.MsgData.Content, &tmp)
					content = tmp.Content
				}

				// 获取redis连接
				redisClient := m.MsgDatabase.GetRedis()
				sensitive := live.NewSensitive(redisClient, content)
				SenCfg := sensitive.GetSensitiveConfig()
				flag, _ := strconv.Atoi(SenCfg.Flag)
				if content != "" {
					sentence, keywords, found := sensitive.Filter()
					if found {
						// 推送命中队列
						var hitMessage live.HitSensitiveMessage
						hitMessage.From = req.MsgData.SendID
						hitMessage.Target = req.MsgData.RecvID
						hitMessage.Type = 0
						if req.MsgData.SessionType == 3 {
							hitMessage.Type = 1
							hitMessage.Target = req.MsgData.GroupID
						}
						hitMessage.DT = time.Now().Unix()
						hitMessage.Content = content
						hitMessage.IP = req.MsgData.Ip
						type sensitiveWord struct {
							SensitiveWords []string `json:"sensitiveWords"`
						}
						extra, _ := json.Marshal(sensitiveWord{SensitiveWords: keywords})
						hitMessage.Extra = string(extra)
						sensitive.PushToHitMQ(hitMessage)

						// 处理结果
						switch flag {
						case 1, 2: // 直接返回 发送失败
							return nil, errs.ErrMsgSensitiveWordFailed.Wrap("Cause by sensitive word.")
						case 3: // 敏感词替换
							var data []byte
							if contentType == 101 {
								var tmp Content1
								tmp.Content = sentence
								data, _ = json.Marshal(tmp)
							} else {
								var tmp Content2
								tmp.Content = sentence
								data, _ = json.Marshal(tmp)
							}
							req.MsgData.Content = data
							req.MsgData.OfflinePushInfo.Desc = sentence
						}
					}
				}
			}

		}

		m.encapsulateMsgData(req.MsgData)
		switch req.MsgData.SessionType {
		case constant.SingleChatType:
			return m.sendMsgSingleChat(ctx, req)
		case constant.NotificationChatType:
			return m.sendMsgNotification(ctx, req)
		case constant.SuperGroupChatType:
			return m.sendMsgSuperGroupChat(ctx, req)
		default:
			return nil, errs.ErrArgs.Wrap("unknown sessionType")
		}
	} else {
		return nil, errs.ErrArgs.Wrap("msgData is nil")
	}
}

func (m *msgServer) sendMsgSuperGroupChat(
	ctx context.Context,
	req *pbmsg.SendMsgReq,
) (resp *pbmsg.SendMsgResp, err error) {
	if err = m.messageVerification(ctx, req); err != nil {
		prommetrics.GroupChatMsgProcessFailedCounter.Inc()
		return nil, err
	}
	if err = callbackBeforeSendGroupMsg(ctx, req); err != nil {
		return nil, err
	}
	if err := callbackMsgModify(ctx, req); err != nil {
		return nil, err
	}
	err = m.MsgDatabase.MsgToMQ(ctx, utils.GenConversationUniqueKeyForGroup(req.MsgData.GroupID), req.MsgData)
	if err != nil {
		return nil, err
	}
	if req.MsgData.ContentType == constant.AtText {
		go m.setConversationAtInfo(ctx, req.MsgData)
	}
	if err = callbackAfterSendGroupMsg(ctx, req); err != nil {
		log.ZWarn(ctx, "CallbackAfterSendGroupMsg", err)
	}
	prommetrics.GroupChatMsgProcessSuccessCounter.Inc()
	resp = &pbmsg.SendMsgResp{}
	resp.SendTime = req.MsgData.SendTime
	resp.ServerMsgID = req.MsgData.ServerMsgID
	resp.ClientMsgID = req.MsgData.ClientMsgID
	return resp, nil
}

func (m *msgServer) setConversationAtInfo(nctx context.Context, msg *sdkws.MsgData) {
	log.ZDebug(nctx, "setConversationAtInfo", "msg", msg)
	ctx := mcontext.NewCtx("@@@" + mcontext.GetOperationID(nctx))
	var atUserID []string
	conversation := &pbconversation.ConversationReq{
		ConversationID:   msgprocessor.GetConversationIDByMsg(msg),
		ConversationType: msg.SessionType,
		GroupID:          msg.GroupID,
	}
	tagAll := utils.IsContain(constant.AtAllString, msg.AtUserIDList)
	if tagAll {
		memberUserIDList, err := m.Group.GetGroupMemberIDs(ctx, msg.GroupID)
		if err != nil {
			log.ZWarn(ctx, "GetGroupMemberIDs", err)
			return
		}
		atUserID = utils.DifferenceString([]string{constant.AtAllString}, msg.AtUserIDList)
		if len(atUserID) == 0 { // just @everyone
			conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtAll}
		} else { //@Everyone and @other people
			conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtAllAtMe}
			err := m.Conversation.SetConversations(ctx, atUserID, conversation)
			if err != nil {
				log.ZWarn(ctx, "SetConversations", err, "userID", atUserID, "conversation", conversation)
			}
			memberUserIDList = utils.DifferenceString(atUserID, memberUserIDList)
		}
		conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtAll}
		err = m.Conversation.SetConversations(ctx, memberUserIDList, conversation)
		if err != nil {
			log.ZWarn(ctx, "SetConversations", err, "userID", memberUserIDList, "conversation", conversation)
		}
	} else {
		conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.AtMe}
		err := m.Conversation.SetConversations(ctx, msg.AtUserIDList, conversation)
		if err != nil {
			log.ZWarn(ctx, "SetConversations", err, msg.AtUserIDList, conversation)
		}
	}
}

func (m *msgServer) sendMsgNotification(
	ctx context.Context,
	req *pbmsg.SendMsgReq,
) (resp *pbmsg.SendMsgResp, err error) {
	if err := m.MsgDatabase.MsgToMQ(ctx, utils.GenConversationUniqueKeyForSingle(req.MsgData.SendID, req.MsgData.RecvID), req.MsgData); err != nil {
		return nil, err
	}
	resp = &pbmsg.SendMsgResp{
		ServerMsgID: req.MsgData.ServerMsgID,
		ClientMsgID: req.MsgData.ClientMsgID,
		SendTime:    req.MsgData.SendTime,
	}
	return resp, nil
}

func (m *msgServer) sendMsgSingleChat(ctx context.Context, req *pbmsg.SendMsgReq) (resp *pbmsg.SendMsgResp, err error) {
	if err := m.messageVerification(ctx, req); err != nil {
		return nil, err
	}
	isSend := true
	isNotification := msgprocessor.IsNotificationByMsg(req.MsgData)
	if !isNotification {
		isSend, err = m.modifyMessageByUserMessageReceiveOpt(
			ctx,
			req.MsgData.RecvID,
			utils.GenConversationIDForSingle(req.MsgData.SendID, req.MsgData.RecvID),
			constant.SingleChatType,
			req,
		)
		if err != nil {
			return nil, err
		}
	}
	if !isSend {
		prommetrics.SingleChatMsgProcessFailedCounter.Inc()
		return nil, nil
	} else {
		if err = callbackBeforeSendSingleMsg(ctx, req); err != nil {
			return nil, err
		}
		if err := callbackMsgModify(ctx, req); err != nil {
			return nil, err
		}
		if err := m.MsgDatabase.MsgToMQ(ctx, utils.GenConversationUniqueKeyForSingle(req.MsgData.SendID, req.MsgData.RecvID), req.MsgData); err != nil {
			prommetrics.SingleChatMsgProcessFailedCounter.Inc()
			return nil, err
		}
		err = callbackAfterSendSingleMsg(ctx, req)
		if err != nil {
			log.ZWarn(ctx, "CallbackAfterSendSingleMsg", err, "req", req)
		}
		resp = &pbmsg.SendMsgResp{
			ServerMsgID: req.MsgData.ServerMsgID,
			ClientMsgID: req.MsgData.ClientMsgID,
			SendTime:    req.MsgData.SendTime,
		}
		prommetrics.SingleChatMsgProcessSuccessCounter.Inc()
		return resp, nil
	}
}

func (m *msgServer) BatchSendMsg(ctx context.Context, in *pbmsg.BatchSendMessageReq) (*pbmsg.BatchSendMessageResp, error) {
	return nil, nil
}

func (m *msgServer) MsgIdGetConversations(ctx context.Context, req *pbmsg.MsgIdGetConversationsReq) (*pbmsg.MsgIdGetConversationsResp, error) {
	resp := &pbmsg.MsgIdGetConversationsResp{}
	if err := authverify.CheckAccessV3(ctx, req.FromUserID); err != nil {
		return nil, err
	}
	var conversationIds map[string]string
	conversationIds, err := m.MsgDatabase.MsgIdGetConversations(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.ConversationIDs = conversationIds
	return resp, nil
}

func (m *msgServer) MsgIdGetConversationSeq(ctx context.Context, req *pbmsg.MsgIdGetConversationSeqReq) (*pbmsg.MsgIdGetConversationSeqResp, error) {
	resp := &pbmsg.MsgIdGetConversationSeqResp{}
	resp, err := m.MsgDatabase.MsgIdGetConversationSeq(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
