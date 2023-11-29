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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func telegramSendMessage(token string, chat string, text string) (string, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{
		"chat_id":    chat,
		"text":       text,
		"parse_mode": "HTML",
	}); err != nil {
		return "", err
	}
	resp, err := http.Post(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token),
		"application/json",
		&buf,
	)
	if err != nil {
		return "", err
	}
	return telegramParseResponse(resp)
}

func telegramSendAudioStream(token string, chat string, audio string, audioReader io.Reader, audioWriter io.Writer, text string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for key, val := range map[string]string{
		"chat_id":    chat,
		"caption":    text,
		"parse_mode": "HTML",
	} {
		part, err := w.CreateFormField(key)
		if err != nil {
			return "", err
		}
		if _, err = io.Copy(part, strings.NewReader(val)); err != nil {
			return "", err
		}
	}
	part, err := w.CreateFormFile("audio", audio)
	if err != nil {
		return "", err
	}
	if audioWriter != nil {
		_, err = io.Copy(io.MultiWriter(part, audioWriter), audioReader)
	} else {
		_, err = io.Copy(part, audioReader)
	}
	if err != nil {
		return "", err
	}
	if err = w.Close(); err != nil {
		return "", err
	}
	resp, err := http.Post(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendAudio", token),
		w.FormDataContentType(),
		&buf,
	)
	if err != nil {
		return "", err
	}
	return telegramParseResponse(resp)
}

func telegramParseResponse(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	result := make(map[string]any)
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	//e := json.NewEncoder(os.Stdout)
	//e.SetIndent("", "  ")
	//if err := e.Encode(result); err != nil {
	//	return "", err
	//}
	if ok, _ := result["ok"].(bool); !ok {
		code, _ := result["error_code"].(float64)
		desc, _ := result["description"].(string)
		if desc == "" {
			desc = "unknown error"
		}
		return "", fmt.Errorf("telegram request error %d: %s", int(code), desc)
	}
	if result, ok := result["result"].(map[string]any); ok {
		if id, ok := result["message_id"].(float64); ok {
			return strconv.Itoa(int(id)), nil
		}
	}
	return "?", nil
}

type telegramUser struct {
	Id       int    `json:"id"`
	Username string `json:"username"`
}

type telegramChat struct {
	Id int `json:"id"`
}

type telegramMessage struct {
	From telegramUser `json:"from"`
	Chat telegramChat `json:"chat"`
	Text string       `json:"text"`
	Date int64        `json:"date"`
}

type telegramUpdate struct {
	UpdateId int             `json:"update_id"`
	Message  telegramMessage `json:"message"`
}

func telegramGetUpdates(token string, offset int) ([]*telegramUpdate, error) {
	resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d", token, offset+1))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var updates []*telegramUpdate
	for {
		var u telegramUpdate
		if err = json.NewDecoder(resp.Body).Decode(&u); err != nil {
			if err == io.EOF {
				return updates, nil
			}
			return nil, err
		}
		updates = append(updates, &u)
	}
}

func telegramListenBot(cfg *config, f func() ([]taskResult, error)) error {
	users := make(map[int]struct{})
	for _, u := range cfg.BotUsers {
		users[u] = struct{}{}
	}

	offset := 0
	startTime := time.Now().Unix()

	interval := 10 * time.Second
	if cfg.BotRefreshInterval != 0 {
		interval = time.Duration(cfg.BotRefreshInterval) * time.Second
	}
	errnum := 0

	log.Println("listening...")

	for {
		reqs, err := func() (map[int]struct{}, error) {
			updates, err := telegramGetUpdates(cfg.TelegramBotToken, offset)
			if err != nil {
				return nil, err
			}
			log.Printf("received %d updates\n", len(updates))
			reqs := make(map[int]struct{})
			for _, u := range updates {

				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				enc.Encode(u)

				if u.UpdateId == 0 {
					log.Println("update_id = 0")
					continue
				}
				offset = u.UpdateId
				if u.Message.Date < startTime {
					log.Println("bad time")
					continue
				}
				if _, ok := users[u.Message.From.Id]; !ok {
					log.Println("bad user")
					continue
				}
				if u.Message.Text != cfg.BotTriggerMessage {
					log.Println("bad message")
					continue
				}
				reqs[u.Message.Chat.Id] = struct{}{}
			}
			return reqs, nil
		}()

		if err != nil {
			log.Printf("listening error: %v\n", err)
			if errnum++; errnum > cfg.BotMaxErrors {
				return err
			}
		} else {
			errnum = 0
			if len(reqs) != 0 {
				log.Printf("received %d sync requests\n", len(reqs))

				for chat := range reqs {
					if _, err = telegramSendMessage(cfg.TelegramBotToken, strconv.Itoa(chat), "starting sync..."); err != nil {
						log.Println(err)
					}
				}

				log.Println("starting sync...")
				report := ""
				if results, err := f(); err != nil {
					report = fmt.Sprintf("sync failed: %v", err)
				} else {
					for _, result := range results {
						report += result.name + "\n"
						if result.err != nil {
							report += fmt.Sprintf("error: %s\n", err)
						}
						report += fmt.Sprintf("records: total %d, done %d, failed %d\n", result.total, result.done, result.failed)
					}
				}

				log.Println(report)

				for chat := range reqs {
					if _, err = telegramSendMessage(cfg.TelegramBotToken, strconv.Itoa(chat), report); err != nil {
						log.Println(err)
					}
				}
			}
		}

		time.Sleep(interval)
	}
}
