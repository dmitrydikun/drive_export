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
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type target interface {
	ID() string
	Type() string
	Name() string

	Insert(row map[string]string, fs *drive.FilesService) (string, error)
	//Update(row map[string]string, fs *drive.FilesService) (error)
	Finish() error
}

func newTarget(cfg *config, tcfg *targetConfig, tdir string) (target, error) {
	switch tcfg.Type {
	case telegramTargetType:
		return newTelegramTarget(tcfg, cfg.TelegramBotToken, tdir)
	case htmlCatalogTargetType:
		return newHTMLCatalogTarget(tcfg, tdir)
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

func copyRow(row map[string]string) map[string]string {
	row2 := make(map[string]string, len(row))
	for k, v := range row {
		row2[k] = v
	}
	return row2
}

const telegramTargetType = "telegram"

type telegramTarget struct {
	taskDir  string
	name     string
	token    string
	channel  string
	template *template.Template
}

func newTelegramTarget(cfg *targetConfig, token string, tdir string) (target, error) {
	tmpl, err := template.ParseFiles(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %v", err)
	}
	return &telegramTarget{
		taskDir:  tdir,
		name:     cfg.Name,
		token:    token,
		channel:  cfg.TelegramChannel,
		template: tmpl,
	}, nil
}

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
	row = copyRow(row)
	var buf bytes.Buffer
	if err := tt.template.Execute(&buf, row); err != nil {
		return "", fmt.Errorf("failed to render template: %v", err)
	}
	if aname, ok := row["audio"]; ok && aname != "" {
		tadir := filepath.Join(tt.taskDir, "audio")
		tafile := filepath.Join(tadir, aname)
		if _, err := os.Stat(tafile); err != nil {
			if !os.IsNotExist(err) {
				return "", err
			}
			id, err := getDriveFileId(fs, aname, "")
			if err != nil {
				return "", err
			}
			rc, err := getDriveFileReadCloser(fs, id, "")
			if err != nil {
				return "", err
			}
			defer rc.Close()
			if err = os.MkdirAll(tadir, dirPerm); err != nil {
				return "", err
			}
			taf, err := os.OpenFile(tafile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
			if err != nil {
				return "", err
			}
			defer taf.Close()
			defer taf.Sync()
			return telegramSendAudioStream(tt.token, tt.channel, aname, rc, taf, buf.String())
		} else {
			taf, err := os.OpenFile(tafile, os.O_RDONLY, 0)
			if err != nil {
				return "", err
			}
			defer taf.Close()
			return telegramSendAudioStream(tt.token, tt.channel, aname, taf, nil, buf.String())
		}
		//id, err := getDriveFileId(fs, audio, "")
		//if err != nil {
		//	return "", err
		//}
		//rc, err := getDriveFileReadCloser(fs, id, "")
		//if err != nil {
		//	return "", err
		//}
		//defer rc.Close()
		//return telegramSendAudioStream(tt.token, tt.channel, audio, rc, buf.String())
	} else {
		return telegramSendMessage(tt.token, tt.channel, buf.String())
	}
}

func (tt *telegramTarget) Finish() error {
	return nil
}

const htmlCatalogTargetType = "html_catalog"

type htmlCatalogTarget struct {
	taskDir      string
	name         string
	catalog      string
	catalogDir   string
	catalogIndex string
	tmpIndex     string
	indexBuf     []byte
	lastId       int
	template     *template.Template
	//prefix           string
	indexPlaceholder string
}

func newHTMLCatalogTarget(cfg *targetConfig, tdir string) (target, error) {
	if cfg.IndexPlaceholder == "" {
		return nil, errors.New("invalid config: index placeholder not set")
	}
	cdir := filepath.Join(cfg.Dir, cfg.Catalog)
	if err := os.MkdirAll(cdir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create catalog directory: %v", err)
	}
	idxfile := filepath.Join(cdir, "index.html")
	idxbuf, err := os.ReadFile(idxfile)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read catalog index: %v", err)
		}
		idxbuf = []byte(fmt.Sprintf("<ul>%s</ul>", cfg.IndexPlaceholder))
		if err = os.WriteFile(idxfile, idxbuf, filePerm); err != nil {
			return nil, fmt.Errorf("failed to create catalog index: %v", err)
		}
	}
	tmpl, err := template.ParseFiles(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %v", err)
	}
	maxId := 0
	if dirents, err := os.ReadDir(cdir); err != nil {
		return nil, fmt.Errorf("failed to read catalog directory: %v", err)
	} else {
		for _, dirent := range dirents {
			if id, err := strconv.Atoi(dirent.Name()); err == nil && maxId < id {
				maxId = id
			}
		}
	}
	t := &htmlCatalogTarget{
		taskDir:      tdir,
		name:         cfg.Name,
		catalog:      cfg.Catalog,
		catalogDir:   cdir,
		catalogIndex: idxfile,
		indexBuf:     idxbuf,
		lastId:       maxId,
		template:     tmpl,
		//prefix:           strings.Trim(cfg.Prefix, "/"),
		indexPlaceholder: cfg.IndexPlaceholder,
	}
	t.tmpIndex = filepath.Join(tdir, t.ID()+"_index.html")
	return t, nil
}

func (ct *htmlCatalogTarget) ID() string {
	return htmlCatalogTargetType + "_" + ct.name
}

func (ct *htmlCatalogTarget) Type() string {
	return htmlCatalogTargetType
}

func (ct *htmlCatalogTarget) Name() string {
	return ct.name
}

func (ct *htmlCatalogTarget) Insert(row map[string]string, fs *drive.FilesService) (string, error) {
	row = copyRow(row)

	title := row["title"]
	if title == "" {
		return "", errors.New("invalid row: no title")
	}
	text := row["text"]
	if text == "" {
		return "", errors.New("invalid row: no text")
	}
	row["text"] = strings.ReplaceAll(
		"<p>"+strings.ReplaceAll(text, "\n", "</p><p>")+"</p>",
		"<p></p>",
		"",
	)

	id := strconv.Itoa(ct.lastId + 1)
	idir := filepath.Join(ct.catalogDir, id)
	if err := os.MkdirAll(idir, dirPerm); err != nil {
		return "", err
	}
	if err := func() error {
		if aname, ok := row["audio"]; ok && aname != "" {
			tadir := filepath.Join(ct.taskDir, "audio")
			tafile := filepath.Join(tadir, aname)
			iafile := filepath.Join(idir, aname)
			if _, err := os.Stat(tafile); err != nil {
				if !os.IsNotExist(err) {
					return err
				}
				id, err := getDriveFileId(fs, aname, "")
				if err != nil {
					return err
				}
				rc, err := getDriveFileReadCloser(fs, id, "")
				if err != nil {
					return err
				}
				defer rc.Close()
				if err = os.MkdirAll(tadir, dirPerm); err != nil {
					return err
				}
				taf, err := os.OpenFile(tafile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
				if err != nil {
					return err
				}
				defer taf.Close()
				defer taf.Sync()
				iaf, err := os.OpenFile(iafile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
				if err != nil {
					return err
				}
				defer iaf.Close()
				defer iaf.Sync()
				if _, err := io.Copy(io.MultiWriter(taf, iaf), rc); err != nil {
					return err
				}
			} else {
				taf, err := os.OpenFile(tafile, os.O_RDONLY, 0)
				if err != nil {
					return err
				}
				defer taf.Close()
				iaf, err := os.OpenFile(iafile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
				if err != nil {
					return err
				}
				defer iaf.Close()
				defer iaf.Sync()
				if _, err := io.Copy(iaf, taf); err != nil {
					return err
				}
			}
			row["audio"] = fmt.Sprintf("//%s/%s/%s", ct.catalog, id, aname)
		}
		f, err := os.OpenFile(filepath.Join(idir, "index.html"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
		if err != nil {
			return err
		}
		defer f.Close()
		defer f.Sync()
		if err = ct.template.Execute(f, row); err != nil {
			return fmt.Errorf("failed to render template: %v", err)
		}
		ct.indexBuf = bytes.Replace(ct.indexBuf, []byte(ct.indexPlaceholder),
			[]byte(fmt.Sprintf(`<li><a href='/%s?item=%s'>%s</a></li>`, ct.name, id, title)+ct.indexPlaceholder), 1)
		if err = os.WriteFile(ct.tmpIndex, ct.indexBuf, filePerm); err != nil {
			return err
		}
		if err = os.Rename(ct.tmpIndex, ct.catalogIndex); err != nil {
			return err
		}
		ct.lastId++
		return nil
	}(); err != nil {
		_ = os.RemoveAll(idir)
		return "", err
	}
	return id, nil
}

func (ct *htmlCatalogTarget) Finish() error {
	return nil
}
