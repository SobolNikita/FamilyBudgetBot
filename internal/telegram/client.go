package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Incoming struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		ID   int64  `json:"message_id"`
		Text string `json:"text"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func New(token string) *Client {
	return &Client{BaseURL: "https://api.telegram.org/bot" + token, HTTP: &http.Client{Timeout: 40 * time.Second}}
}

func (client *Client) call(ctx context.Context, method string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.BaseURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot construct Telegram request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.HTTP.Do(request)
	if err != nil {
		return errors.New("Telegram request failed")
	}
	defer response.Body.Close()
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&envelope); err != nil {
		if response.StatusCode != http.StatusOK {
			return &APIError{Status: response.StatusCode}
		}
		return errors.New("invalid Telegram response")
	}
	if response.StatusCode != http.StatusOK || !envelope.OK {
		status := envelope.ErrorCode
		if status == 0 {
			status = response.StatusCode
		}
		if method == "editMessageText" && status == 400 && strings.Contains(envelope.Description, "message is not modified") {
			return nil
		}
		missing := method == "editMessageText" && status == 400 && (strings.Contains(envelope.Description, "message to edit not found") || strings.Contains(envelope.Description, "message can't be edited"))
		return &APIError{Status: status, RetryAfter: time.Duration(min(max(envelope.Parameters.RetryAfter, 0), 86400)) * time.Second, MissingMessage: missing}
	}
	if target != nil {
		return json.Unmarshal(envelope.Result, target)
	}
	return nil
}

type APIError struct {
	Status         int
	RetryAfter     time.Duration
	MissingMessage bool
}

func (failure *APIError) Error() string {
	return fmt.Sprintf("Telegram returned HTTP %d", failure.Status)
}

func (client *Client) Poll(ctx context.Context, offset int64) ([]Incoming, error) {
	var updates []Incoming
	err := client.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "limit": 50, "allowed_updates": []string{"message"}}, &updates)
	return updates, err
}

func (client *Client) Send(ctx context.Context, chatID int64, text string) error {
	_, err := client.Reply(ctx, chatID, 0, text)
	return err
}

func (client *Client) Reply(ctx context.Context, chatID, originalID int64, text string) (int64, error) {
	payload := map[string]any{"chat_id": chatID, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}
	if originalID > 0 {
		payload["reply_parameters"] = map[string]any{"message_id": originalID, "allow_sending_without_reply": true}
	}
	var result struct {
		ID int64 `json:"message_id"`
	}
	err := client.call(ctx, "sendMessage", payload, &result)
	if err == nil && result.ID <= 0 {
		err = errors.New("Telegram response has no message ID")
	}
	return result.ID, err
}

func (client *Client) Edit(ctx context.Context, chatID, messageID int64, text string) error {
	return client.call(ctx, "editMessageText", map[string]any{"chat_id": chatID, "message_id": messageID, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}, nil)
}
