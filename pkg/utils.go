package pkg

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const (
	CONTEXT_PREFIX = ":."
	CONTEXT_SUFFIX = ":"
)

func ExecuteCommand(name string, args []string, silence bool) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var oBuffer bytes.Buffer
	cmd.Stdout = &oBuffer
	if !silence {
		cmd.Stderr = os.Stderr
	}
	err := cmd.Run()
	if err != nil {
		return nil, err
	}
	return oBuffer.Bytes(), nil
}

func IsKeySchema(subject string) bool {
	return strings.HasSuffix(subject, "-key")
}

func IsValueSchema(subject string) bool {
	return strings.HasSuffix(subject, "-value")
}

func VerifySubject(subject string) bool {
	return IsValueSchema(subject) || IsKeySchema(subject)
}

func ExtractTopicFromSubject(subjects []string) []string {
	var topics []string
	for _, subject := range subjects {
		var rawSubject string
		if !strings.HasPrefix(subject, CONTEXT_PREFIX) {
			rawSubject = subject
		} else {
			rawSubject = subject[len(CONTEXT_PREFIX):]
			idx := strings.Index(rawSubject, CONTEXT_SUFFIX)
			if idx == -1 {
				rawSubject = subject
			} else {
				rawSubject = rawSubject[idx+1:]
			}
		}
		topic := strings.TrimSuffix(strings.TrimSuffix(rawSubject, "-value"), "-key")
		topics = append(topics, topic)
	}
	return topics
}

func ValidateParams(cmd *cobra.Command) (string, bool, error) {
	if !cmd.Flags().Changed("all") && !cmd.Flags().Changed("subject") {
		return "", false, errors.New("at least one of --subject or --all must be specified")
	}
	if cmd.Flags().Changed("all") && cmd.Flags().Changed("subject") {
		return "", false, errors.New("only one of --subject or --all can be specified")
	}

	if cmd.Flags().Changed("subject") {
		subject, err := cmd.Flags().GetString("subject")
		if err != nil {
			return "", false, err
		}
		if !VerifySubject(subject) {
			return "", false, errors.New("only subjects from TopicNameStrategy is supported")
		}
	}
	subject, err := cmd.Flags().GetString("subject")
	if err != nil {
		return "", false, err
	}
	cleanAll, err := cmd.Flags().GetBool("all")
	if err != nil {
		return "", false, err
	}
	return subject, cleanAll, nil
}

func PrintTable(fields []interface{}, objects interface{}, includeOrder bool) {
	if includeOrder {
		fields = append([]interface{}{""}, fields...)
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(fields)

	for idx, object := range ConvertToInterfaceArray(objects) {
		var elements []interface{}
		if includeOrder {
			elements = append(elements, idx)
		}
		r := reflect.ValueOf(object)
		for i, field := range fields {
			if i == 0 && includeOrder {
				continue
			}
			f := reflect.Indirect(r).FieldByName(field.(string))
			elements = append(elements, f)
		}
		t.AppendRow(elements)
	}
	t.AppendSeparator()
	t.Render()
}

func ConvertToInterfaceArray(object interface{}) []interface{} {
	o := reflect.ValueOf(object)

	slice := make([]interface{}, o.Len())
	for i := 0; i < o.Len(); i++ {
		slice[i] = o.Index(i).Interface()
	}
	return slice
}

func ContainsTopic(topic string, topics []string) bool {
	for _, t := range topics {
		if topic == t {
			return true
		}
	}
	return false
}

func IsYes(resp string) bool {
	return resp == "Y" || resp == "y" || resp == "yes" || resp == "Yes" || resp == "YES"
}

func IsNo(resp string) bool {
	return resp == "N" || resp == "n" || resp == "no" || resp == "No" || resp == "NO"
}

func IsValidChoice(resp string) bool {
	return IsYes(resp) || IsNo(resp)
}

func ResetColor() {
	fmt.Print(RESET)
}

func ReadLine() (string, error) {
	str, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(str, "\r\n"), err
}
