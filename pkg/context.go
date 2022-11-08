package pkg

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/havoc-io/gopass"
)

type Context struct {
	Credentials map[string]Credentials
	Clusters    []string
	Subjects    []string
	Topics      []string
}

func NewContext(configFile string) (*Context, error) {
	var err error
	credentials := make(map[string]Credentials)
	if len(configFile) != 0 {
		credentials, err = loadConfig(configFile)
		if err != nil {
			return nil, err
		}
	}
	return &Context{
		Credentials: credentials,
	}, nil
}

func (ctx *Context) SetClusters(clusters []string) error {
	if err := ctx.promptCredentials(clusters); err != nil {
		return err
	}
	ctx.Clusters = clusters
	return nil
}

func loadConfig(configFile string) (map[string]Credentials, error) {
	content, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	var credentials map[string]Credentials
	if err = json.Unmarshal(content, &credentials); err != nil {
		return nil, err
	}
	return credentials, nil
}

func (ctx *Context) promptCredentials(clusters []string) error {
	for _, cluster := range clusters {
		if _, ok := ctx.Credentials[cluster]; ok {
			continue
		}
		fmt.Printf("Enter your API Key for Kafka cluster %s: ", cluster)
		apiKey, err := ReadLine()
		if err != nil {
			return err
		}
		fmt.Printf("Enter your API secret for Kafka cluster %s: ", cluster)
		apiSecret, err := gopass.GetPasswdMasked()
		if err != nil {
			return err
		}
		ctx.Credentials[cluster] = Credentials{apiKey, string(apiSecret)}
	}

	return nil
}
