package intent

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type intentChatModelFunc func(context.Context) (*schema.Message, error)

func (function intentChatModelFunc) Generate(
	ctx context.Context,
	_ []*schema.Message,
	_ ...model.Option,
) (*schema.Message, error) {
	return function(ctx)
}

func TestLLMRecognizerRejectsInvalidJSON(t *testing.T) {
	recognizer, err := NewLLMRecognizer(
		intentChatModelFunc(func(context.Context) (*schema.Message, error) {
			return schema.AssistantMessage("not-json", nil), nil
		}),
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewLLMRecognizer() error = %v", err)
	}
	if _, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "checkout",
	}); err == nil {
		t.Fatal("Recognize() error = nil, want invalid JSON error")
	}
}

func TestLLMRecognizerHonorsTimeout(t *testing.T) {
	recognizer, err := NewLLMRecognizer(
		intentChatModelFunc(func(ctx context.Context) (*schema.Message, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
		10*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("NewLLMRecognizer() error = %v", err)
	}
	started := time.Now()
	if _, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "checkout",
	}); err == nil {
		t.Fatal("Recognize() error = nil, want timeout")
	}
	if time.Since(started) > time.Second {
		t.Fatalf("timeout was not bounded: %s", time.Since(started))
	}
}
