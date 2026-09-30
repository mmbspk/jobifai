package tasks

import (
	"fmt"
	"strings"

	"github.com/user/jobifai/internal/eval/dataset"
	"github.com/user/jobifai/internal/llm"
	"github.com/user/jobifai/internal/resume"
)

// MessagesForCase builds the same provider messages production sends for this eval case.
func MessagesForCase(task string, c dataset.Case) ([]llm.Message, string, error) {
	msgs, err := resume.ProviderMessages(task, c.Input)
	if err != nil {
		return nil, "", err
	}
	var b strings.Builder
	for _, m := range msgs {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Content)
	}
	return msgs, b.String(), nil
}

// MessagesForCaseOrError wraps MessagesForCase with task label in errors.
func MessagesForCaseOrError(task string, c dataset.Case) ([]llm.Message, error) {
	msgs, _, err := MessagesForCase(task, c)
	if err != nil {
		return nil, fmt.Errorf("%s case %s: %w", task, c.ID, err)
	}
	return msgs, nil
}
