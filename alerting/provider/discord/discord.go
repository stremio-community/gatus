package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/TwiN/gatus/v5/alerting/alert"
	"github.com/TwiN/gatus/v5/client"
	"github.com/TwiN/gatus/v5/config/endpoint"
	"gopkg.in/yaml.v3"
)

var (
	ErrWebhookURLNotSet                   = errors.New("webhook-url not set")
	ErrDuplicateGroupOverride             = errors.New("duplicate group override")
	ErrEditOnResolvedRequiresComponentsV2 = errors.New("edit-on-resolved requires components-v2")
)

type Config struct {
	WebhookURL     string   `yaml:"webhook-url"`
	Title          string   `yaml:"title,omitempty"`            // Title of the message that will be sent
	MessageContent string   `yaml:"message-content,omitempty"`  // Message content for pinging users or groups (e.g. "<@123456789>" or "<@&987654321>")
	ComponentsV2   bool     `yaml:"components-v2,omitempty"`    // Whether to send the alert as Components V2 instead of an embed
	StatusPageURL  string   `yaml:"status-page-url,omitempty"`  // Base URL of the status page, used to link a Components V2 alert to its endpoint
	DetailLabels   []string `yaml:"detail-labels,omitempty"`    // Labels of the condition result details a Components V2 alert shows under a failed condition
	EditOnResolved bool     `yaml:"edit-on-resolved,omitempty"` // Whether to mark the triggered Components V2 alert as recovered instead of sending a new message
}

func (cfg *Config) Validate() error {
	if len(cfg.WebhookURL) == 0 {
		return ErrWebhookURLNotSet
	}
	if cfg.EditOnResolved && !cfg.ComponentsV2 {
		return ErrEditOnResolvedRequiresComponentsV2
	}
	return nil
}

func (cfg *Config) Merge(override *Config) {
	if len(override.WebhookURL) > 0 {
		cfg.WebhookURL = override.WebhookURL
	}
	if len(override.Title) > 0 {
		cfg.Title = override.Title
	}
	if len(override.MessageContent) > 0 {
		cfg.MessageContent = override.MessageContent
	}
	if override.ComponentsV2 {
		cfg.ComponentsV2 = true
	}
	if len(override.StatusPageURL) > 0 {
		cfg.StatusPageURL = override.StatusPageURL
	}
	if len(override.DetailLabels) > 0 {
		cfg.DetailLabels = override.DetailLabels
	}
	if override.EditOnResolved {
		cfg.EditOnResolved = true
	}
}

// requestURL returns the URL of path under the webhook, with params added to the webhook's own query, such as a thread_id.
// Discord rejects components sent to a webhook that isn't owned by an application unless with_components is set.
func (cfg *Config) requestURL(path string, params url.Values) string {
	if !cfg.ComponentsV2 && len(path) == 0 && len(params) == 0 {
		return cfg.WebhookURL
	}
	webhookURL, err := url.Parse(cfg.WebhookURL)
	if err != nil {
		return cfg.WebhookURL
	}
	webhookURL.Path = strings.TrimSuffix(webhookURL.Path, "/") + path
	query := webhookURL.Query()
	for key, values := range params {
		query[key] = values
	}
	if cfg.ComponentsV2 {
		query.Set("with_components", "true")
	}
	webhookURL.RawQuery = query.Encode()
	return webhookURL.String()
}

// AlertProvider is the configuration necessary for sending an alert using Discord
type AlertProvider struct {
	DefaultConfig Config `yaml:",inline"`

	// DefaultAlert is the default alert configuration to use for endpoints with an alert of the appropriate type
	DefaultAlert *alert.Alert `yaml:"default-alert,omitempty"`

	// Overrides is a list of Override that may be prioritized over the default configuration
	Overrides []Override `yaml:"overrides,omitempty"`
}

type Override struct {
	Group  string `yaml:"group"`
	Config `yaml:",inline"`
}

// Validate the provider's configuration
func (provider *AlertProvider) Validate() error {
	registeredGroups := make(map[string]bool)
	if provider.Overrides != nil {
		for _, override := range provider.Overrides {
			if isAlreadyRegistered := registeredGroups[override.Group]; isAlreadyRegistered || override.Group == "" || len(override.WebhookURL) == 0 {
				return ErrDuplicateGroupOverride
			}
			registeredGroups[override.Group] = true
		}
	}
	return provider.DefaultConfig.Validate()
}

// Send an alert using the provider
func (provider *AlertProvider) Send(ep *endpoint.Endpoint, alert *alert.Alert, result *endpoint.Result, resolved bool) error {
	cfg, err := provider.GetConfig(ep.Group, alert)
	if err != nil {
		return err
	}
	if resolved && cfg.EditOnResolved && len(alert.ResolveKey) > 0 {
		messageID := alert.ResolveKey
		alert.ResolveKey = ""
		if edited, err := editAsResolved(cfg, messageID, alert, result); edited {
			return err
		}
	}
	// With wait, Discord answers with the message, whose ID is kept to edit it once the alert is resolved.
	keepMessageID := cfg.EditOnResolved && !resolved
	var params url.Values
	if keepMessageID {
		params = url.Values{"wait": {"true"}}
	}
	statusCode, responseBody, err := request(http.MethodPost, cfg.requestURL("", params), provider.buildRequestBody(cfg, ep, alert, result, resolved))
	if statusCode == http.StatusBadRequest && cfg.ComponentsV2 && len(result.Logo) > 0 {
		// Discord validates the thumbnail URL, which comes from a third party, so drop it rather than the alert.
		withoutLogo := *result
		withoutLogo.Logo = ""
		_, responseBody, err = request(http.MethodPost, cfg.requestURL("", params), provider.buildRequestBody(cfg, ep, alert, &withoutLogo, resolved))
	}
	if err == nil && keepMessageID {
		var message Message
		if json.Unmarshal(responseBody, &message) == nil {
			alert.ResolveKey = message.ID
		}
	}
	return err
}

// editAsResolved marks the message of a triggered alert as recovered, and returns whether that message was dealt with.
// A message that was deleted counts as dealt with, while one this provider can't recognize is left for a new message to resolve.
func editAsResolved(cfg *Config, messageID string, alert *alert.Alert, result *endpoint.Result) (bool, error) {
	messagePath := "/messages/" + url.PathEscape(messageID)
	statusCode, responseBody, err := request(http.MethodGet, cfg.requestURL(messagePath, nil), nil)
	if statusCode == http.StatusNotFound {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	var message Message
	if err := json.Unmarshal(responseBody, &message); err != nil {
		return false, nil
	}
	subtext := findSubtext(message.Components)
	if subtext == nil {
		return false, nil
	}
	subtext.Content = "-# " + buildRecoveredSubtext(alert, result, messageTimestamp(messageID))
	container := findContainer(message.Components)
	container.AccentColor = colorResolved
	body, _ := json.Marshal(Message{Components: message.Components})
	statusCode, _, err = request(http.MethodPatch, cfg.requestURL(messagePath, nil), body)
	if statusCode == http.StatusNotFound {
		return true, nil
	}
	return true, err
}

func request(method, requestURL string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, requestURL, bytes.NewBuffer(body))
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.GetHTTPClient(nil).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode > 399 {
		return response.StatusCode, responseBody, fmt.Errorf("call to provider alert returned status code %d: %s", response.StatusCode, string(responseBody))
	}
	return response.StatusCode, responseBody, nil
}

type Body struct {
	Content string  `json:"content"`
	Embeds  []Embed `json:"embeds"`
}

type Embed struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Color       int     `json:"color"`
	Fields      []Field `json:"fields,omitempty"`
}

type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// buildRequestBody builds the request body for the provider
func (provider *AlertProvider) buildRequestBody(cfg *Config, ep *endpoint.Endpoint, alert *alert.Alert, result *endpoint.Result, resolved bool) []byte {
	if cfg.ComponentsV2 {
		return buildComponentsV2RequestBody(cfg, ep, alert, result, resolved)
	}
	var message string
	var colorCode int
	if resolved {
		message = fmt.Sprintf("An alert for **%s** has been resolved after passing successfully %d time(s) in a row", ep.DisplayName(), alert.SuccessThreshold)
		colorCode = 3066993
	} else {
		message = fmt.Sprintf("An alert for **%s** has been triggered due to having failed %d time(s) in a row", ep.DisplayName(), alert.FailureThreshold)
		colorCode = 15158332
	}
	var formattedConditionResults string
	for _, conditionResult := range result.ConditionResults {
		var prefix string
		if conditionResult.Success {
			prefix = ":white_check_mark:"
		} else {
			prefix = ":x:"
		}
		formattedConditionResults += fmt.Sprintf("%s - `%s`\n", prefix, conditionResult.Condition)
	}
	var description string
	if alertDescription := alert.GetDescription(); len(alertDescription) > 0 {
		description = ":\n> " + alertDescription
	}
	title := ":helmet_with_white_cross: Gatus"
	if cfg.Title != "" {
		title = cfg.Title
	}
	body := Body{
		Content: cfg.MessageContent,
		Embeds: []Embed{
			{
				Title:       title,
				Description: message + description,
				Color:       colorCode,
			},
		},
	}
	if len(formattedConditionResults) > 0 {
		body.Embeds[0].Fields = append(body.Embeds[0].Fields, Field{
			Name:   "Condition results",
			Value:  formattedConditionResults,
			Inline: false,
		})
	}
	bodyAsJSON, _ := json.Marshal(body)
	return bodyAsJSON
}

const (
	componentTypeActionRow   = 1
	componentTypeButton      = 2
	componentTypeSection     = 9
	componentTypeTextDisplay = 10
	componentTypeThumbnail   = 11
	componentTypeContainer   = 17

	buttonStyleLink = 5

	messageFlagIsComponentsV2 = 1 << 15

	colorTriggered = 15158332
	colorResolved  = 3066993

	maximumConditionLines          = 10
	maximumDetailLinesPerCondition = 5
	maximumDetailValueLength       = 150

	// conditionsCharacterBudget keeps an alert well under Discord's 4000 character limit on a message's text, past which Discord rejects it.
	// Pushed detail values may be far longer than the ones shown, so the cap on lines alone doesn't guarantee that.
	conditionsCharacterBudget = 3500
)

// thumbnailAlignmentPadding is a line of blanks, wider than most headings, under a heading that has a thumbnail.
// Discord sizes a container to its widest line and pins the thumbnail to its right edge, so without it the thumbnail shifts with the text.
// It uses U+2800, which renders blank but isn't whitespace Discord could trim.
var thumbnailAlignmentPadding = "-# " + strings.Repeat("\u2800", 90)

type ComponentsV2Body struct {
	Flags      int         `json:"flags"`
	Components []Component `json:"components"`
}

type Component struct {
	Type        int         `json:"type"`
	Content     string      `json:"content,omitempty"`
	AccentColor int         `json:"accent_color,omitempty"`
	Components  []Component `json:"components,omitempty"`
	Style       int         `json:"style,omitempty"`
	Label       string      `json:"label,omitempty"`
	URL         string      `json:"url,omitempty"`
	Accessory   *Component  `json:"accessory,omitempty"`
	Media       *Media      `json:"media,omitempty"`
}

type Media struct {
	URL string `json:"url"`
}

// Message is the part of a Discord message this provider reads back, and the body of an edit to it.
type Message struct {
	ID         string      `json:"id,omitempty"`
	Components []Component `json:"components"`
}

// discordEpoch is the start of time in a Discord snowflake ID, in milliseconds since the Unix epoch.
const discordEpoch = 1420070400000

// messageTimestamp returns when a message was sent, which its snowflake ID encodes, or the zero time when the ID doesn't parse.
func messageTimestamp(messageID string) time.Time {
	snowflake, err := strconv.ParseUint(messageID, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(int64(snowflake>>22) + discordEpoch)
}

func findContainer(components []Component) *Component {
	for i := range components {
		if components[i].Type == componentTypeContainer {
			return &components[i]
		}
	}
	return nil
}

// findSubtext returns the subtext under the heading of a message built by buildComponentsV2RequestBody, or nil when it has none.
func findSubtext(components []Component) *Component {
	container := findContainer(components)
	if container == nil || len(container.Components) == 0 {
		return nil
	}
	header := container.Components
	if header[0].Type == componentTypeSection {
		header = header[0].Components
	}
	if len(header) < 2 || header[1].Type != componentTypeTextDisplay || !strings.HasPrefix(header[1].Content, "-# ") || header[1].Content == thumbnailAlignmentPadding {
		return nil
	}
	return &header[1]
}

func buildComponentsV2RequestBody(cfg *Config, ep *endpoint.Endpoint, alert *alert.Alert, result *endpoint.Result, resolved bool) []byte {
	color := colorTriggered
	if resolved {
		color = colorResolved
	}
	heading := fmt.Sprintf("**%s**", ep.Name)
	if group := strings.TrimSpace(ep.Group); len(group) > 0 {
		heading += " · " + group
	}
	header := []Component{{Type: componentTypeTextDisplay, Content: heading}}
	if subtext := buildSubtext(alert, result); len(subtext) > 0 {
		header = append(header, Component{Type: componentTypeTextDisplay, Content: "-# " + subtext})
	}
	if len(result.Logo) > 0 {
		header = []Component{{
			Type:       componentTypeSection,
			Components: append(header, Component{Type: componentTypeTextDisplay, Content: thumbnailAlignmentPadding}),
			Accessory:  &Component{Type: componentTypeThumbnail, Media: &Media{URL: result.Logo}},
		}}
	}
	container := Component{
		Type:        componentTypeContainer,
		AccentColor: color,
		Components:  header,
	}
	if !resolved {
		if conditions := formatConditions(result, cfg.DetailLabels); len(conditions) > 0 {
			container.Components = append(container.Components, Component{Type: componentTypeTextDisplay, Content: conditions})
		}
	}
	if len(cfg.StatusPageURL) > 0 {
		container.Components = append(container.Components, Component{
			Type: componentTypeActionRow,
			Components: []Component{{
				Type:  componentTypeButton,
				Style: buttonStyleLink,
				Label: "View on status page",
				URL:   strings.TrimSuffix(cfg.StatusPageURL, "/") + "/endpoints/" + url.PathEscape(ep.Key()),
			}},
		})
	}
	body := ComponentsV2Body{Flags: messageFlagIsComponentsV2}
	if len(cfg.MessageContent) > 0 {
		body.Components = append(body.Components, Component{Type: componentTypeTextDisplay, Content: cfg.MessageContent})
	}
	body.Components = append(body.Components, container)
	bodyAsJSON, _ := json.Marshal(body)
	return bodyAsJSON
}

func buildSubtext(alert *alert.Alert, result *endpoint.Result) string {
	var parts []string
	if description := alert.GetDescription(); len(description) > 0 {
		parts = append(parts, description)
	} else if len(result.Hostname) > 0 {
		parts = append(parts, result.Hostname)
	}
	if result.Duration > 0 {
		parts = append(parts, formatDuration(result.Duration))
	}
	if !result.Timestamp.IsZero() {
		parts = append(parts, fmt.Sprintf("<t:%d:R>", result.Timestamp.Unix()))
	}
	return strings.Join(parts, " · ")
}

func buildRecoveredSubtext(alert *alert.Alert, result *endpoint.Result, triggeredAt time.Time) string {
	var parts []string
	if description := alert.GetDescription(); len(description) > 0 {
		parts = append(parts, description)
	} else if len(result.Hostname) > 0 {
		parts = append(parts, result.Hostname)
	}
	resolvedAt := result.Timestamp
	if resolvedAt.IsZero() {
		resolvedAt = time.Now()
	}
	parts = append(parts, fmt.Sprintf("✅ Recovered <t:%d:R>", resolvedAt.Unix()))
	if !triggeredAt.IsZero() && resolvedAt.After(triggeredAt) {
		parts = append(parts, "down for "+formatDowntime(resolvedAt.Sub(triggeredAt)))
	}
	return strings.Join(parts, " · ")
}

func formatDowntime(downtime time.Duration) string {
	minutes := int(downtime.Round(time.Minute).Minutes())
	switch {
	case minutes < 1:
		return "<1m"
	case minutes < 60:
		return fmt.Sprintf("%dm", minutes)
	case minutes < 24*60:
		return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
	default:
		return fmt.Sprintf("%dd %dh", minutes/(24*60), minutes%(24*60)/60)
	}
}

func formatDuration(duration time.Duration) string {
	if duration < time.Second {
		return duration.Round(time.Millisecond).String()
	}
	return duration.Round(10 * time.Millisecond).String()
}

// formatConditions lists failed conditions before passing ones, each failed one followed by its details whose label is in detailLabels.
func formatConditions(result *endpoint.Result, detailLabels []string) string {
	var failed, passed []*endpoint.ConditionResult
	for _, conditionResult := range result.ConditionResults {
		if conditionResult.Success {
			passed = append(passed, conditionResult)
		} else {
			failed = append(failed, conditionResult)
		}
	}
	var blocks []string
	var length, omitted int
	for _, conditionResult := range append(failed, passed...) {
		block := formatCondition(conditionResult, detailLabels)
		blockLength := utf8.RuneCountInString(block) + 1
		if omitted > 0 || len(blocks) == maximumConditionLines || length+blockLength > conditionsCharacterBudget {
			omitted++
			continue
		}
		blocks = append(blocks, block)
		length += blockLength
	}
	if omitted > 0 {
		blocks = append(blocks, fmt.Sprintf("+%d more", omitted))
	}
	return strings.Join(blocks, "\n")
}

func formatCondition(conditionResult *endpoint.ConditionResult, detailLabels []string) string {
	if conditionResult.Success {
		return fmt.Sprintf("✅ `%s`", conditionResult.Condition)
	}
	lines := []string{fmt.Sprintf("❌ `%s`", conditionResult.Condition)}
	var shown, omitted int
	for _, detail := range conditionResult.Details {
		if detail == nil || !slices.Contains(detailLabels, detail.Label) {
			continue
		}
		if shown == maximumDetailLinesPerCondition {
			omitted++
			continue
		}
		lines = append(lines, fmt.Sprintf("-# %s: `%s`", detail.Label, formatDetailValue(detail.Value)))
		shown++
	}
	if omitted > 0 {
		lines = append(lines, fmt.Sprintf("-# +%d more", omitted))
	}
	return strings.Join(lines, "\n")
}

// formatDetailValue fits a value on one line of inline code, which a backtick would end and a newline would break.
func formatDetailValue(value string) string {
	value = strings.NewReplacer("`", "'", "\r", " ", "\n", " ").Replace(value)
	if runes := []rune(value); len(runes) > maximumDetailValueLength {
		return string(runes[:maximumDetailValueLength-1]) + "…"
	}
	return value
}

// GetDefaultAlert returns the provider's default alert configuration
func (provider *AlertProvider) GetDefaultAlert() *alert.Alert {
	return provider.DefaultAlert
}

// GetConfig returns the configuration for the provider with the overrides applied
func (provider *AlertProvider) GetConfig(group string, alert *alert.Alert) (*Config, error) {
	cfg := provider.DefaultConfig
	// Handle group overrides
	if provider.Overrides != nil {
		for _, override := range provider.Overrides {
			if group == override.Group {
				cfg.Merge(&override.Config)
				break
			}
		}
	}
	// Handle alert overrides
	if len(alert.ProviderOverride) != 0 {
		overrideConfig := Config{}
		if err := yaml.Unmarshal(alert.ProviderOverrideAsBytes(), &overrideConfig); err != nil {
			return nil, err
		}
		cfg.Merge(&overrideConfig)
	}
	// Validate the configuration
	err := cfg.Validate()
	return &cfg, err
}

// ValidateOverrides validates the alert's provider override and, if present, the group override
func (provider *AlertProvider) ValidateOverrides(group string, alert *alert.Alert) error {
	_, err := provider.GetConfig(group, alert)
	return err
}
