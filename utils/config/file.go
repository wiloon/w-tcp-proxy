package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const sysEnvKeyAppConfig = "app_config"

var defaultFileName = "config.toml"

func LoadDefaultConfigFile() ([]byte, error) {
	return LoadLocalConfig(defaultFileName)
}
func LoadLocalConfig(configFileName string) ([]byte, error) {
	log.Println("loading config file")
	defaultFileName = configFileName

	configFilePath = configPath()
	if !isFileExist(configFilePath) {
		log.Println("config file not found:", configFilePath)
		return nil, errors.New(fmt.Sprintf("config file not found: %s", configFilePath))
	}

	b, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		fmt.Print(err)
	}
	str := string(b)
	log.Printf("config file content: \n%s\n", str)
	return b, nil
}

func configPath() string {
	path := os.Getenv(sysEnvKeyAppConfig)
	if strings.EqualFold(path, "") || !isFileExist(getConfigFilePath(path)) {
		log.Printf("system env key not found, key: %v", sysEnvKeyAppConfig)

		path = execPath()
		if strings.EqualFold(path, "") || !isFileExist(getConfigFilePath(path)) {
			path = currentPath()
		}
	}
	path = filepath.Join(path, defaultFileName)
	log.Println("config file path:", path)
	return path
}

func getConfigFilePath(configPath string) string {
	return filepath.Join(configPath, defaultFileName)
}

func isFileExist(filePath string) bool {
	_, err := os.Stat(filePath)
	fileExist := err == nil || os.IsExist(err)

	log.Printf("file: %s, exist:%v", filePath, fileExist)
	return fileExist
}

func execPath() string {
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}
	return strings.Replace(dir, "\\", "/", -1)
}

func currentPath() string {
	path, _ := os.Getwd()
	return path
}
