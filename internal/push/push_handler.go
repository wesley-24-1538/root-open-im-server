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

package push

import (
	"context"
	"github.com/IBM/sarama"
	"github.com/OpenIMSDK/protocol/constant"
	pbconversation "github.com/OpenIMSDK/protocol/conversation"
	pbchat "github.com/OpenIMSDK/protocol/msg"
	pbpush "github.com/OpenIMSDK/protocol/push"
	"github.com/OpenIMSDK/protocol/wrapperspb"
	"github.com/OpenIMSDK/tools/log"
	"github.com/OpenIMSDK/tools/utils"
	"github.com/zeromicro/go-zero/core/threading"
	"google.golang.org/protobuf/proto"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	kfk "github.com/openimsdk/open-im-server/v3/pkg/common/kafka"
)

type ConsumerHandler struct {
	pushConsumerGroup *kfk.MConsumerGroup
	pusher            *Pusher
}

func NewConsumerHandler(pusher *Pusher) *ConsumerHandler {
	var consumerHandler ConsumerHandler
	consumerHandler.pusher = pusher
	consumerHandler.pushConsumerGroup = kfk.NewMConsumerGroup(&kfk.MConsumerGroupConfig{
		KafkaVersion:   sarama.V2_0_0_0,
		OffsetsInitial: sarama.OffsetNewest, IsReturnErr: false,
	}, []string{config.Config.Kafka.MsgToPush.Topic}, config.Config.Kafka.Addr,
		config.Config.Kafka.ConsumerGroupID.MsgToPush)
	return &consumerHandler
}

func (c *ConsumerHandler) handleMs2PsChat(ctx context.Context, msg []byte) {
	msgFromMQ := pbchat.PushMsgDataToMQ{}
	if err := proto.Unmarshal(msg, &msgFromMQ); err != nil {
		log.ZError(ctx, "push Unmarshal msg err", err, "msg", string(msg))
		return
	}
	pbData := &pbpush.PushMsgReq{
		MsgData:        msgFromMQ.MsgData,
		ConversationID: msgFromMQ.ConversationID,
	}
	sec := msgFromMQ.MsgData.SendTime / 1000
	nowSec := utils.GetCurrentTimestampBySecond()
	log.ZDebug(ctx, "push msg", "msg", pbData.String(), "sec", sec, "nowSec", nowSec)
	if nowSec-sec > 30 {
		return
	}
	var err error
	switch msgFromMQ.MsgData.SessionType {
	case constant.SuperGroupChatType:
		err = c.pusher.Push2SuperGroup(ctx, pbData.MsgData.GroupID, pbData.MsgData)
	default:
		var pushUserIDList []string
		isSenderSync := utils.GetSwitchFromOptions(pbData.MsgData.Options, constant.IsSenderSync)
		if !isSenderSync || pbData.MsgData.SendID == pbData.MsgData.RecvID {
			pushUserIDList = append(pushUserIDList, pbData.MsgData.RecvID)
		} else {
			pushUserIDList = append(pushUserIDList, pbData.MsgData.RecvID, pbData.MsgData.SendID)
		}
		err = c.pusher.Push2User(ctx, pushUserIDList, pbData.MsgData)
	}
	if err != nil {
		if err == errNoOfflinePusher {
			log.ZWarn(ctx, "offline push failed", err, "msg", pbData.String())
		} else {
			log.ZError(ctx, "push failed", err, "msg", pbData.String())
		}
	}
	if strings.Contains(msgFromMQ.ConversationID, "sg_") == false && strings.Contains(msgFromMQ.ConversationID, "si_") == false {
		return
	}

	//更新 latest_msg_send_time
	userIDs := make([]string, 0, 1)
	userIDs = append(userIDs, msgFromMQ.MsgData.SendID)
	req := &pbconversation.ConversationReq{}
	req.ConversationID = msgFromMQ.ConversationID
	req.ConversationType = msgFromMQ.MsgData.SessionType
	req.GroupID = msgFromMQ.MsgData.GroupID
	req.UserID = msgFromMQ.MsgData.SendID
	req.LatestMsgSendTime = wrapperspb.Int64(msgFromMQ.MsgData.SendTime)
	if err := c.pusher.conversationRpcClient.SetConversations(ctx, userIDs, req); err != nil {
		log.ZWarn(ctx, "update latest_msg_send_time failed", err, "msg", pbData.String(), "userIDs", userIDs, "req", req)
	}
}
func (ConsumerHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (ConsumerHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }
func (c *ConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
) error {
	for msg := range claim.Messages() {
		ctx := c.pushConsumerGroup.GetContextFromMsg(msg)
		value := msg.Value
		sess.MarkMessage(msg, "")
		threading.GoSafe(func() {
			c.handleMs2PsChat(ctx, value)
		})
	}
	return nil
}
