package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sgnraft/insider-case/internal/domain"
)

// ProviderResponse is the payload returned by the external webhook.
type ProviderResponse struct {
	MessageID string `json:"messageId"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// Provider sends notifications to an external HTTP webhook.
type Provider struct {
	webhookURL string
	client     *http.Client
	logger     *slog.Logger
}

// New creates a new Provider.
func New(webhookURL string, timeout time.Duration, logger *slog.Logger) *Provider {
	return &Provider{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: timeout},
		logger:     logger,
	}
}

// Send delivers a notification to the external provider.
func (p *Provider) Send(ctx context.Context, n *domain.Notification) (*ProviderResponse, error) {
	body, err := json.Marshal(domain.ProviderRequest{
		To:      n.Recipient,
		Channel: n.Channel,
		Content: n.Content,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Notification-ID", n.ID)
	req.Header.Set("X-Channel", n.Channel)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http send: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider status %d: %s", resp.StatusCode, string(respBody))
	}

	var provResp ProviderResponse
	if err := json.Unmarshal(respBody, &provResp); err != nil || provResp.MessageID == "" {
		// Webhook.site may echo back a non-standard body; synthesise a response.
		provResp = ProviderResponse{
			MessageID: fmt.Sprintf("syn-%s-%d", n.ID[:8], time.Now().UnixNano()),
			Status:    "accepted",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		p.logger.Warn("non-standard provider response, using synthetic ID",
			"notification_id", n.ID, "body", string(respBody))
	}

	p.logger.Info("notification sent to provider",
		"notification_id", n.ID,
		"provider_msg_id", provResp.MessageID,
		"channel", n.Channel,
	)
	return &provResp, nil
}
