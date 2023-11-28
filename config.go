// Copyright 2023 Dmitry Dikun
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"os"
)

type config struct {
	DataDir               string        `json:"data_dir"`
	GoogleCredentialsFile string        `json:"google_credentials_file"`
	GoogleTokenFile       string        `json:"google_token_file"`
	TelegramBotToken      string        `json:"telegram_bot_token"`
	Tasks                 []*taskConfig `json:"tasks"`
}

type taskConfig struct {
	Name    string          `json:"name"`
	File    string          `json:"file"`
	Targets []*targetConfig `json:"targets"`
}

type targetConfig struct {
	Type             string `json:"type"`
	Name             string `json:"name"`
	Dir              string `json:"dir"`
	Catalog          string `json:"catalog"`
	TelegramChannel  string `json:"telegram_channel"`
	Template         string `json:"template"`
	IndexPlaceholder string `json:"index_placeholder"`
	StaticPrefix     string `json:"static_prefix"`
}

func readConfig() (*config, error) {
	file := os.Args[0] + ".json"
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var cfg config
	if err = json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
