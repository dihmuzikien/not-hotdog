package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/awslabs/aws-lambda-go-api-proxy/handlerfunc"
	"go.uber.org/zap"
	"net/http"
	"os"
)

type detector interface {
	doesLabelMatch(image []byte, label string) (bool, error)
}

type server struct {
	line    *linebot.Client
	labeler detector
	logger     *zap.Logger
}


func formatMatched(label string) string{
	return fmt.Sprintf("YES - this is a %v", label)
}

func formatNotMatch(label string) string{
	return fmt.Sprintf("No - this isn't a %v", label)
}

func (s *server) notImageHandler(message *linebot.ImageMessage, replyToken string) error {
	content, err := s.line.GetMessageContent(message.ID).Do()
	label := "dog"
	if err != nil {
		s.logger.Info("handling image message", zap.String("imageUrl", message.OriginalContentURL))
		return fmt.Errorf("failed to get image content %v ", err)
	}
	defer content.Content.Close()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(content.Content)
	if err != nil {
		s.logger.Info("unable to read image content")
		return fmt.Errorf("unable to read image content %v ", err)
	}
	match, err := s.labeler.doesLabelMatch(buf.Bytes(), label)
	if err != nil {
		s.logger.Error("failed to detect labels", zap.Error(err))
		return fmt.Errorf("cannot detect label %v", err)
	}
	if !match {
		_, err := s.line.ReplyMessage(replyToken, linebot.NewTextMessage(formatNotMatch(label))).Do()
		return err
	}
	_, err = s.line.ReplyMessage(replyToken, linebot.NewTextMessage(formatMatched(label))).Do()
	return err

}

func (s *server) handleHook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := s.line.ParseRequest(r)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse requests %v", err), 500)
		}
		for _, event := range request {
			switch message := event.Message.(type) {
			case *linebot.ImageMessage:
				if err := s.notImageHandler(message, event.ReplyToken); err != nil {
					s.logger.Error("failure in image handler: %v", zap.Error(err))
				}
			default:
				s.logger.Warn("unsupported event type: %v", zap.String("type", string(event.Type)))
			}
			}
		w.WriteHeader(http.StatusOK)
		return
	}
}

func (s *server) lambdaHandler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	h := handlerfunc.NewV2(s.handleHook())
	return h.ProxyWithContext(ctx, req)
}


func newServer(logger *zap.Logger) *server {
	bot, err := linebot.New(os.Getenv("NOT_HOTDOG_CHANNEL_SECRET"),os.Getenv("NOT_HOTDOG_CHANNEL_TOKEN"))
	if err != nil {
		panic(err)
	}
	return &server{
		logger: logger,
		labeler: newRekognitionImpl(),
		line: bot,
	}
}

func main(){
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	s := newServer(logger)
	lambda.Start(s.lambdaHandler)
}
