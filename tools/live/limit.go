package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OpenIMSDK/tools/log"
	"github.com/redis/go-redis/v9"
	"strconv"
	"time"
)

type BrushLimit struct {
	redis redis.UniversalClient
}
type (
	//BrushConfig 刷屏配置
	BrushConfig struct {
		BrushTime       string `json:"im_brush_time"`        //单位时间x
		BrushLimit      string `json:"im_brush_limit"`       //单位条数x
		BrushSpeechTime string `json:"im_brush_speech_time"` //禁言x分钟
		BrushBanLimit   string `json:"im_brush_ban_limit"`   //累计禁言次数x
	}

	TriggerUser struct {
		UserId    string `json:"user_id"`
		BrushTime string `json:"brush_time"`
		DataType  string `json:"data_type"`
	}
)

const (
	RedisConfigKey    = "brush_config"                   //刷屏后台配置 key
	RedisUserKey      = "im_brush_user:%s"               //记录-用户 单位时间发送条数
	RedisBlockImKey   = "brush_user_block_im:%s"         //记录im-用户 触发限制次数 key
	RedisBrushAllKey  = "im_super_brush_all_mute:%s"     //用户被禁言 key
	RedisImTriggerKey = "im_brush_user_trigger_push_key" //禁言 MQ key
	RedisImBlockKey   = "im_brush_user_block_push_key"   //禁言拉黑 MQ key

	SendMessageFastError      = "您说话太快啦，休息%s秒吧！"
	DatetimeFormatYYYYMMDDHIS = "2006-01-02 15:04:05"
)

func NewBrushLimit(redisClient redis.UniversalClient) *BrushLimit {
	brushLimit := new(BrushLimit)
	brushLimit.redis = redisClient

	return brushLimit
}

func (bl *BrushLimit) Check(userId string) (bool, error) {
	// 是否被禁言
	superBrushAllRedisKey := format(RedisBrushAllKey, userId)
	muteAll := bl.redis.TTL(context.Background(), superBrushAllRedisKey).Val()
	log.ZDebug(context.Background(), "brushLimit.Check", "muteAll", muteAll, "superBrushAllRedisKey", superBrushAllRedisKey)
	if muteAll > 0 {
		return false, errors.New(format(SendMessageFastError, fmt.Sprintf("%.f", muteAll.Seconds())))
	}
	// 获取禁用设置
	brushConfigByte, err := bl.redis.Get(context.Background(), RedisConfigKey).Bytes()
	if err != nil {
		tips := "获取禁用设置失败"
		log.ZWarn(context.Background(), tips, err)
		return true, errors.New(tips)
	}
	var brushConfig BrushConfig
	err = json.Unmarshal(brushConfigByte, &brushConfig)
	if err != nil {
		tips := "BrushConfig Unmarshal failed"
		log.ZWarn(context.Background(), tips, err)
		return true, errors.New(tips)
	}
	// 有配置刷屏
	brushTime, _ := strconv.Atoi(brushConfig.BrushTime)
	brushLimit, _ := strconv.Atoi(brushConfig.BrushLimit)
	brushBanLimit, _ := strconv.Atoi(brushConfig.BrushBanLimit)
	brushSpeechTime, _ := strconv.Atoi(brushConfig.BrushSpeechTime)

	// 记录单位时间发送条数增加一条
	redisUserKey := format(RedisUserKey, userId)
	messagesNum := bl.redis.Incr(context.Background(), redisUserKey).Val()
	ttl := bl.redis.TTL(context.Background(), redisUserKey).Val()
	if ttl < 0 {
		ttl = time.Duration(brushTime) * time.Second
		bl.redis.Expire(context.Background(), redisUserKey, ttl)
	}
	log.ZDebug(context.Background(), "brushLimit.Check", "brushConfig", brushConfig)
	log.ZDebug(context.Background(), "brushLimit.Check", "redisUserKey", redisUserKey, "messagesNum", messagesNum, "ttl", ttl, "brushLimit", brushLimit)

	// 判断该用户是否达到被拉黑和禁言上限次数
	if messagesNum > int64(brushLimit) {
		// 刷屏禁言该用户
		bl.redis.Set(context.Background(), superBrushAllRedisKey, brushSpeechTime, time.Duration(brushSpeechTime)*time.Minute)

		// 记录改用户触发限制次数
		redisBlockImKey := format(RedisBlockImKey, userId)
		blockNumIm := bl.redis.Incr(context.Background(), redisBlockImKey).Val()
		bl.redis.Persist(context.Background(), redisBlockImKey)

		var triggerUser TriggerUser
		triggerUser.UserId = userId
		triggerUser.BrushTime = time.Now().Format(DatetimeFormatYYYYMMDDHIS)
		triggerUser.DataType = "open_im"
		triggerUserByte, _ := json.Marshal(triggerUser)
		if blockNumIm < int64(brushBanLimit) {
			// 禁言
			bl.redis.LPush(context.Background(), RedisImTriggerKey, string(triggerUserByte))
			bl.redis.Del(context.Background(), redisUserKey)
		} else {
			// 禁言拉黑
			bl.redis.LPush(context.Background(), RedisImBlockKey, string(triggerUserByte))
			bl.redis.Del(context.Background(), redisBlockImKey)
		}
	}

	blockAll := bl.redis.TTL(context.Background(), superBrushAllRedisKey).Val()
	if blockAll > 0 {
		return false, errors.New(format(SendMessageFastError, fmt.Sprintf("%.f", blockAll.Seconds())))
	}

	return true, nil
}

func format(Format string, UserId string) string {
	return fmt.Sprintf(Format, UserId)
}
