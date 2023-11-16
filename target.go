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
	"errors"
	"fmt"
	"google.golang.org/api/drive/v3"
	"html/template"
)

type target interface {
	ID() string
	Type() string
	Name() string
	Insert(row map[string]string, fs *drive.FilesService) (string, error)
	//Update(row map[string]string, fs *drive.FilesService) (error)
}

func newTarget(cfg *config, tcfg *targetConfig, dir string) (target, error) {
	tmpl, err := template.ParseFiles(tcfg.Template)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %v", err)
	}
	switch tcfg.Type {
	case telegramTargetType:
		return &telegramTarget{
			name:     tcfg.Name,
			dir:      dir,
			token:    cfg.TelegramBotToken,
			channel:  tcfg.TelegramChannel,
			template: tmpl,
		}, nil
	default:
		return nil, errors.New("invalid target")
	}
}

func targetStatusFieldName(t target) string {
	return t.ID() + "_status"
}

func targetRecordIdFieldName(t target) string {
	return t.ID() + "_record_id"
}

type telegramTarget struct {
	name     string
	dir      string
	token    string
	channel  string
	template *template.Template
}

const telegramTargetType = "telegram"

func (tt *telegramTarget) ID() string {
	return telegramTargetType + "_" + tt.name
}

func (tt *telegramTarget) Type() string {
	return telegramTargetType
}

func (tt *telegramTarget) Name() string {
	return tt.name
}

func (tt *telegramTarget) Insert(row map[string]string, fs *drive.FilesService) (string, error) {
	var buf bytes.Buffer
	if err := tt.template.Execute(&buf, row); err != nil {
		return "", fmt.Errorf("failed to render template: %v", err)
	}
	if audio, ok := row["audio"]; ok && audio != "" {
		//dir := filepath.Join(tt.dir, "audio")
		//file := filepath.Join(dir, audio)
		//if _, err := os.Stat(file); err != nil {
		//	if !os.IsNotExist(err) {
		//		return err
		//	}
		//	if err = os.MkdirAll(dir, dirPerm); err != nil {
		//		return err
		//	}
		//	if _, err = downloadDriveFile(fs, audio, file); err != nil {
		//		return fmt.Errorf("failed to download audio: %v", err)
		//	}
		//}
		//return telegramSendAudio(tt.token, tt.channel, file, buf.String())
		id, err := getDriveFileId(fs, audio, "")
		if err != nil {
			return "", err
		}
		rc, err := getDriveFileReadCloser(fs, id, "")
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return telegramSendAudio(tt.token, tt.channel, audio, rc, buf.String())
	} else {
		return telegramSendMessage(tt.token, tt.channel, buf.String())
	}
}
