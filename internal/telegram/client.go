package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Incoming struct {
	ID      int64 `json:"update_id"`
	Message *struct {
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
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram returned HTTP %d", response.StatusCode)
	}
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&envelope); err != nil {
		return errors.New("invalid Telegram response")
	}
	if !envelope.OK {
		return errors.New("Telegram rejected request")
	}
	if target != nil {
		return json.Unmarshal(envelope.Result, target)
	}
	return nil
}

func (client *Client) Poll(ctx context.Context, offset int64) ([]Incoming, error) {
	var updates []Incoming
	err := client.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "limit": 50, "allowed_updates": []string{"message"}}, &updates)
	return updates, err
}

func (client *Client) Send(ctx context.Context, chatID int64, text string) error {
	return client.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}, nil)
}
