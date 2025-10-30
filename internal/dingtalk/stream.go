package dingtalk

import (
	"context"
	"fmt"
	"log"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/logger"
)

type StreamClient struct {
	client         *client.StreamClient
	messageHandler MessageHandler
}

type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *IncomingMessage) error
	HandleCardCallback(ctx context.Context, callback *CardCallback) error
}

type IncomingMessage struct {
	ConversationID            string `json:"conversationId"`
	ChatbotCorpID             string `json:"chatbotCorpId"`
	ChatbotUserID             string `json:"chatbotUserId"`
	MsgID                     string `json:"msgId"`
	SenderNick                string `json:"senderNick"`
	IsAdmin                   bool   `json:"isAdmin"`
	SenderStaffID             string `json:"senderStaffId"`
	SessionWebhookExpiredTime int64  `json:"sessionWebhookExpiredTime"`
	CreateAt                  int64  `json:"createAt"`
	SenderCorpID              string `json:"senderCorpId"`
	ConversationType          string `json:"conversationType"`
	SenderID                  string `json:"senderId"`
	ConversationTitle         string `json:"conversationTitle"`
	IsInAtList                bool   `json:"isInAtList"`
	SessionWebhook            string `json:"sessionWebhook"`
	Text                      struct {
		Content string `json:"content"`
	} `json:"text"`
	RobotCode string `json:"robotCode"`
	MsgType   string `json:"msgtype"`
	AtUsers   []struct {
		DingtalkID string `json:"dingtalkId"`
		StaffID    string `json:"staffId"`
	} `json:"atUsers"`
}

type CardCallback struct {
	OutTrackID string `json:"outTrackId"`
	CorpID     string `json:"corpId"`
	UserID     string `json:"userId"`
	Value      string `json:"value"`
}

func NewStreamClient(appKey, appSecret string, handler MessageHandler) *StreamClient {
	// 设置日志级别
	logger.SetLogger(logger.NewStdTestLogger())

	streamClient := client.NewStreamClient(
		client.WithAppCredential(client.NewAppCredentialConfig(appKey, appSecret)),
	)

	return &StreamClient{
		client:         streamClient,
		messageHandler: handler,
	}
}

func (s *StreamClient) Start(ctx context.Context) error {
	// 注册群消息回调（v0.9.1 期望的签名为 IChatBotMessageHandler）
	s.client.RegisterChatBotCallbackRouter(s.onBotMessage)

	// 启动 Stream 客户端
	if err := s.client.Start(ctx); err != nil {
		return fmt.Errorf("启动 Stream 客户端失败: %w", err)
	}

	log.Println("✓ 钉钉 Stream 客户端已启动")
	return nil
}

// v0.9.1: 回调函数需返回 ([]byte, error)
func (s *StreamClient) onBotMessage(ctx context.Context, df *chatbot.BotCallbackDataModel) ([]byte, error) {
	// 直接使用 SDK 解析好的模型
	log.Printf("收到消息: [%s] %s: %s", df.ConversationTitle, df.SenderNick, df.Text.Content)

	// 映射到业务层使用的 IncomingMessage（便于与现有 Handler 解耦）
	var msg IncomingMessage
	msg.ConversationID = df.ConversationId
	msg.ChatbotCorpID = df.ChatbotCorpId
	msg.ChatbotUserID = df.ChatbotUserId
	msg.MsgID = df.MsgId
	msg.SenderNick = df.SenderNick
	msg.IsAdmin = df.IsAdmin
	msg.SenderStaffID = df.SenderStaffId
	msg.SessionWebhookExpiredTime = df.SessionWebhookExpiredTime
	msg.CreateAt = df.CreateAt
	msg.SenderCorpID = df.SenderCorpId
	msg.ConversationType = df.ConversationType
	msg.SenderID = df.SenderId
	msg.ConversationTitle = df.ConversationTitle
	msg.IsInAtList = df.IsInAtList
	msg.SessionWebhook = df.SessionWebhook
	msg.Text.Content = df.Text.Content
	msg.MsgType = df.Msgtype
	if len(df.AtUsers) > 0 {
		for _, u := range df.AtUsers {
			msg.AtUsers = append(msg.AtUsers, struct {
				DingtalkID string `json:"dingtalkId"`
				StaffID    string `json:"staffId"`
			}{DingtalkID: u.DingtalkId, StaffID: u.StaffId})
		}
	}

	// 业务处理
	if err := s.messageHandler.HandleMessage(ctx, &msg); err != nil {
		log.Printf("处理消息失败: %v", err)
		// 直接返回文本，由 SDK 回复到会话
		return []byte(fmt.Sprintf("❌ 处理失败: %v", err)), nil
	}

	// 直接返回文本，由 SDK 回复到会话
	return []byte("✅ 收到"), nil
}

func (s *StreamClient) Stop() {
	if s.client != nil {
		s.client.Close()
	}
}
