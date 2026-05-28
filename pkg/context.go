package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/term"
)

type Context struct {
	Credentials map[string]Credentials
	Clusters    []string
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

func (ctx *Context) SetClusters(clusters []string, force bool) error {
	if err := ctx.resolveCredentials(clusters, force); err != nil {
		return err
	}
	ctx.Clusters = clusters
	return nil
}

func loadConfig(configFile string) (map[string]Credentials, error) {
	content, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	var credentials map[string]Credentials
	if err = json.Unmarshal(content, &credentials); err != nil {
		return nil, err
	}
	return credentials, nil
}

func (ctx *Context) resolveCredentials(clusters []string, force bool) error {
	for _, cluster := range clusters {
		if _, ok := ctx.Credentials[cluster]; ok {
			continue
		}
		if force {
			return fmt.Errorf("no credentials for cluster %s; provide them via --config-file when using --force", cluster)
		}
		fmt.Printf("Enter your API Key for Kafka cluster %s: ", cluster)
		apiKey, err := ReadLine()
		if err != nil {
			return err
		}
		fmt.Printf("Enter your API secret for Kafka cluster %s: ", cluster)
		apiSecret, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return err
		}
		ctx.Credentials[cluster] = Credentials{apiKey, string(apiSecret)}
	}

	return nil
}
