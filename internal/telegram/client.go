package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// TelegramClient определяет интерфейс для работы с Telegram Bot API.
// Это позволяет легко создавать моки для тестирования.
type TelegramClient interface {
	SendMessage(ctx context.Context, chatID string, text string, parseMode string) error
	GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error)
}

// Client инкапсулирует работу с Telegram Bot API.
type Client struct {
	token  string
	client *http.Client
	apiURL string
}

// Убеждаемся, что Client реализует интерфейс TelegramClient.
var _ TelegramClient = (*Client)(nil)

// NewClient создаёт клиента. token обязателен.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		apiURL: fmt.Sprintf("https://api.telegram.org/bot%s", token),
	}
}

// SendMessage отправляет текстовое сообщение.
func (c *Client) SendMessage(ctx context.Context, chatID string, text string, parseMode string) error {
	payload := map[string]string{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}

	return c.post(ctx, "sendMessage", payload, nil)
}

// GetUpdates получает входящие обновления, начиная с offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	params := url.Values{}
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}
	if timeout <= 0 {
		timeout = 5
	}
	params.Set("timeout", fmt.Sprintf("%d", timeout))

	var resp GetUpdatesResponse
	if err := c.get(ctx, "getUpdates", params, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("telegram getUpdates not ok")
	}
	return resp.Result, nil
}

func (c *Client) post(ctx context.Context, method string, body interface{}, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/"+method, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram api status %d", resp.StatusCode)
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) get(ctx context.Context, method string, params url.Values, out interface{}) error {
	u := c.apiURL + "/" + method
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram api status %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
