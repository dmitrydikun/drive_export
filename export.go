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
	tasks map[string]*task
}

const (
	filePerm = 0644
	dirPerm  = 0755
)

func newExport(cfg *config) (*export, error) {
	var err error
	var exp = &export{cfg: cfg}
	exp.dir = filepath.Join(cfg.DataDir, time.Now().Format(time.DateTime))
	if err = os.MkdirAll(exp.dir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create export exportDir: %v", err)
	}
	exp.tasks = make(map[string]*task, len(cfg.Tasks))
	for _, tcfg := range cfg.Tasks {
		if _, ok := exp.tasks[tcfg.Name]; ok {
			return nil, fmt.Errorf("invalid config: duplicated task %s", tcfg.Name)
		}
		t, err := newTask(cfg, tcfg, exp.dir)
		if err != nil {
			return nil, fmt.Errorf("failed to init task %s: %v", tcfg.Name, err)
		}
		exp.tasks[tcfg.Name] = t
	}
	exp.fs, err = getDriveFilesService(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get files service: %v", err)
	}
	return exp, nil
}

func (exp *export) fetch() {
	for name, t := range exp.tasks {
		log.Printf("fetching files for task: %s\n", t.name)
		if err := t.fetch(exp.fs); err != nil {
			log.Printf("fail: %v\n", err)
			delete(exp.tasks, name)
		} else {
			log.Printf("success: %s -> %s\n", t.origin, t.source)
		}
	}
}

func (exp *export) process() []taskResult {
	results := make([]taskResult, len(exp.tasks))
	for _, t := range exp.tasks {
		log.Printf("processing task: %s\n", t.name)
		result := t.process(exp.fs)
		results = append(results, result)
		if result.err != nil {
			log.Printf("fail: %v\n", result.err)
		}
	}
	return results
}

func (exp *export) upload() {
	for _, t := range exp.tasks {
		log.Printf("updating files for task: %s\n", t.name)
		if err := t.update(exp.fs); err != nil {
			log.Printf("fail: %v\n", err)
		}
	}
}

func (exp *export) clean() {
	if err := os.RemoveAll(exp.dir); err != nil {
		log.Print(err)
	}
}
