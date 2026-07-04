package cli

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/team-worapong/wor/internal/output"
	"github.com/team-worapong/wor/internal/setup"
)

type terminalInteractor struct {
	reader                 *bufio.Reader
	renderer               *output.Renderer
	webServerProviderTitle string
	webServerDetections    []setup.Detection
}

func newTerminalInteractor(stdin io.Reader, renderer *output.Renderer) *terminalInteractor {
	return &terminalInteractor{
		reader:   bufio.NewReader(stdin),
		renderer: renderer,
	}
}

func (i *terminalInteractor) Select(prompt string, options []setup.Option, defaultValue string) (string, error) {
	for {
		if isWebServerProviderPrompt(prompt) {
			i.renderWebServerProviderChoices(options, defaultValue)
			answer, _, err := i.readAnswer(selectPrompt(prompt, defaultOptionIndex(options, defaultValue)))
			if err != nil {
				return "", err
			}
			if value, ok := selectAnswerValue(answer, options, defaultValue); ok {
				return value, nil
			}
			i.Warning("invalid selection")
			continue
		}

		i.renderer.Text(prompt)
		defaultIndex := defaultOptionIndex(options, defaultValue)
		for index, option := range options {
			suffix := ""
			if option.Value == defaultValue {
				suffix = " (default)"
			}
			if option.Description != "" {
				i.renderer.Text("%d) %s - %s%s", index+1, option.Label, option.Description, suffix)
				continue
			}
			i.renderer.Text("%d) %s%s", index+1, option.Label, suffix)
		}

		answer, _, err := i.readAnswer(defaultPrompt(defaultIndex))
		if err != nil {
			return "", err
		}
		if value, ok := selectAnswerValue(answer, options, defaultValue); ok {
			return value, nil
		}
		i.Warning("invalid selection")
	}
}

func (i *terminalInteractor) Input(prompt, defaultValue string) (string, error) {
	answer, _, err := i.readAnswer(fmt.Sprintf("%s [%s]", prompt, defaultValue))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(answer) == "" {
		return defaultValue, nil
	}
	return answer, nil
}

func (i *terminalInteractor) Confirm(prompt string, defaultYes bool) (bool, error) {
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}
	for {
		answer, eof, err := i.readAnswer(prompt + " " + suffix)
		if err != nil {
			return false, err
		}
		if eof && strings.TrimSpace(answer) == "" {
			return false, nil
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "" {
			return defaultYes, nil
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			i.Warning("please answer yes or no")
		}
	}
}

func (i *terminalInteractor) ShowDetections(title string, detections []setup.Detection) {
	if isWebServerProviderTitle(title) {
		i.webServerProviderTitle = title
		i.webServerDetections = detections
		return
	}

	i.renderDetectionList(title, detections)
}

func (i *terminalInteractor) ShowSummary(summary setup.Summary) {
	i.renderer.Text("")
	i.renderer.Text("Setup Summary")
	i.renderer.Table(
		[]string{"Key", "Value"},
		[][]string{
			{"Environment", summary.Environment},
			{"WOR_HOME", summary.WORHome},
			{"Web server provider", summary.WebServerProvider},
			{"SSL provider", summary.SSLProvider},
			{"Config file", summary.ConfigFile},
			{"Existing config", fmt.Sprintf("%t", summary.ExistingConfig)},
			{"Dry run", fmt.Sprintf("%t", summary.DryRun)},
		},
	)

	i.renderer.Text("")
	i.renderer.Text("Directories")
	rows := make([][]string, 0, len(summary.Directories))
	for _, dir := range summary.Directories {
		rows = append(rows, []string{dir})
	}
	i.renderer.Table([]string{"Path"}, rows)

	i.renderer.Text("")
	i.renderer.Text("Detected Runtimes")
	i.renderDetectionList("", summary.Detections)
}

func (i *terminalInteractor) Info(message string) {
	i.renderer.Text("%s", message)
}

func (i *terminalInteractor) Warning(message string) {
	i.renderer.Warning("%s", message)
}

func (i *terminalInteractor) readAnswer(prompt string) (string, bool, error) {
	i.renderer.Text("%s", prompt)
	answer, err := i.reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return strings.TrimSpace(answer), true, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(answer), false, nil
}

func defaultPrompt(index int) string {
	if index <= 0 {
		return "Select:"
	}
	return fmt.Sprintf("Select [%d]:", index)
}

func selectPrompt(prompt string, index int) string {
	prompt = strings.TrimSpace(strings.TrimSuffix(prompt, ":"))
	if index <= 0 {
		return prompt + ":"
	}
	return fmt.Sprintf("%s [%d]:", prompt, index)
}

func defaultOptionIndex(options []setup.Option, defaultValue string) int {
	for index, option := range options {
		if option.Value == defaultValue {
			return index + 1
		}
	}
	return 0
}

func selectAnswerValue(answer string, options []setup.Option, defaultValue string) (string, bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		for _, option := range options {
			if option.Value == defaultValue {
				return option.Value, true
			}
		}
		return "", false
	}
	if index, err := strconv.Atoi(answer); err == nil && index >= 1 && index <= len(options) {
		return options[index-1].Value, true
	}
	for _, option := range options {
		if strings.EqualFold(answer, option.Value) || strings.EqualFold(answer, option.Label) {
			return option.Value, true
		}
		for _, alias := range option.Aliases {
			if strings.EqualFold(answer, alias) {
				return option.Value, true
			}
		}
	}
	return "", false
}

func (i *terminalInteractor) renderWebServerProviderChoices(options []setup.Option, defaultValue string) {
	title := i.webServerProviderTitle
	if title == "" {
		title = "Detected Web Server Providers:"
	}

	i.renderer.Text(title)
	for index, option := range options {
		if option.Value == setup.WebServerSkip {
			continue
		}
		detection := webServerProviderDetection(i.webServerDetections, option.Value)
		i.renderer.Text("%d) %s %-7s %s", index+1, detectionIcon(detection), option.Label, detectionChoiceStatus(detection))
	}
	for _, detection := range i.webServerDetections {
		if detection.Name == "IIS" && !detection.Supported {
			i.renderer.Text("-  %s %-7s %s", detectionIcon(detection), "IIS", detectionChoiceStatus(detection))
		}
	}
	for index, option := range options {
		if option.Value != setup.WebServerSkip {
			continue
		}
		status := "default"
		if option.Value != defaultValue {
			status = ""
		}
		i.renderer.Text("%d) - %-7s %s", index+1, option.Label, strings.TrimSpace(status))
	}
}

func (i *terminalInteractor) renderDetectionList(title string, detections []setup.Detection) {
	if strings.TrimSpace(title) != "" {
		i.renderer.Text(title)
	}
	for _, detection := range detections {
		i.renderer.Text("%s %-8s %s", detectionIcon(detection), compactDetectionName(detection.Name), compactDetectionStatus(detection))
	}
}
