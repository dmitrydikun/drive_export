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
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
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

func telegramSendAudio(token string, chat string, audio string, audioReader io.Reader, text string) (string, error) {
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
	if _, err = io.Copy(part, audioReader); err != nil {
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
