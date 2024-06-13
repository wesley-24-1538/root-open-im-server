package live

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/stringx"
)

type SensitiveInterface interface {
	Filter() (sentence string, keywords []string, found bool) //执行过滤
	GetSensitiveConfig() (filterSet SensitiveConfig)          //获取敏感词Redis配置
	GetSensitiveWord() (words []string)                       //获取敏感词库
	PushToHitMQ(hitMessage HitSensitiveMessage)               //推送命中至队列
}

// Sensitive 敏感词类
type Sensitive struct {
	SensitiveInterface
	redis redis.UniversalClient
	word  string
}

const (
	SensitiveConfigKey = "sensitive_filter_set"  //敏感词配置 RedisKey
	SensitiveWordKey   = "sensitive_word"        //敏感词库 RedisKey
	SensitiveHitMqKey  = "sensitive_hit_word_mq" //敏感词命中 RedisKey
)

type (
	// SensitiveConfig 配置struct
	SensitiveConfig struct {
		Flag string `json:"sensitive_filter_set"`
	}
	// SensitiveWords 词库struct
	SensitiveWords struct {
		Id         int64  `json:"id"`
		Word       string `json:"word"`
		CreateTime string `json:"create_time"`
	}

	HitSensitiveMessage struct {
		From    string `json:"from"`           //发送人id
		Type    int    `json:"type"`           //消息类型 0 私聊 1 群聊
		Target  string `json:"target"`         //接受者id
		Content string `json:"searchable_key"` //消息内容
		DT      int64  `json:"dt"`             //命中时间戳
		IP      string `json:"ip"`             //发送者ip
		Extra   string `json:"extra"`          //命中字符 存json格式 列如 {"sensitiveWords":["黑"]}
	}
)

// NewSensitive 初始化
func NewSensitive(redisClient redis.UniversalClient, word string) SensitiveInterface {
	sensitive := new(Sensitive)
	sensitive.redis = redisClient
	sensitive.word = word

	return sensitive
}

// Filter 执行过滤
func (s *Sensitive) Filter() (sentence string, keywords []string, found bool) {
	words := s.GetSensitiveWord()
	if len(words) > 0 {
		trie := stringx.NewTrie(words)
		return trie.Filter(s.word)
	}
	return
}

// GetSensitiveConfig 获取敏感词Redis配置
func (s *Sensitive) GetSensitiveConfig() (filterSet SensitiveConfig) {

	wordByte, err := s.redis.Get(context.Background(), SensitiveConfigKey).Bytes()
	if err != nil {
		return
	}

	_ = json.Unmarshal(wordByte, &filterSet)
	return
}

// GetSensitiveWord 获取敏感词库
func (s *Sensitive) GetSensitiveWord() (returnData []string) {

	var words []SensitiveWords
	wordByte, err := s.redis.Get(context.Background(), SensitiveWordKey).Bytes()
	if err != nil {
		return
	}

	if err = json.Unmarshal(wordByte, &words); err != nil {
		return
	}

	for _, word := range words {
		returnData = append(returnData, word.Word)
	}
	return
}

// PushToHitMQ 推送命中至队列
func (s *Sensitive) PushToHitMQ(hitMessage HitSensitiveMessage) {
	data, _ := json.Marshal(hitMessage)
	s.redis.LPush(context.Background(), SensitiveHitMqKey, string(data))
}
