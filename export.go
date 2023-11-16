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
	"fmt"
	"google.golang.org/api/drive/v3"
	"log"
	"os"
	"path/filepath"
	"time"
)

type export struct {
	cfg   *config
	dir   string
	fs    *drive.FilesService
	items map[string]*item
}

const (
	filePerm = 0600
	dirPerm  = 0700
)

func newExport(cfg *config) (*export, error) {
	var err error
	var exp = &export{cfg: cfg}
	exp.dir = filepath.Join(cfg.DataDir, time.Now().Format(time.DateTime))
	if err = os.MkdirAll(exp.dir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create export dir: %v", err)
	}
	exp.items = make(map[string]*item, len(cfg.Items))
	for _, icfg := range cfg.Items {
		item, err := newItem(cfg, icfg, exp.dir)
		if err != nil {
			return nil, fmt.Errorf("failed to init item %s: %v", icfg.Name, err)
		}
		exp.items[icfg.Name] = item
	}
	exp.fs, err = getDriveFilesService(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get files service: %v", err)
	}
	return exp, nil
}

func (exp *export) fetch() {
	for name, item := range exp.items {
		log.Printf("fetching item: %s\n", item.name)
		if err := item.fetch(exp.fs); err != nil {
			log.Printf("fail: %v\n", err)
			delete(exp.items, name)
		} else {
			log.Printf("success: %s -> %s\n", item.origin, item.source)
		}
	}
}

func (exp *export) process() {
	for _, item := range exp.items {
		log.Printf("processing item: %s\n", item.name)
		if err := item.process(exp.fs); err != nil {
			log.Printf("fail: %v\n", err)
		}
	}
}

func (exp *export) upload() {
	for _, item := range exp.items {
		log.Printf("updating item: %s\n", item.name)
		if err := item.update(exp.fs); err != nil {
			log.Printf("fail: %v\n", err)
		}
	}
}
