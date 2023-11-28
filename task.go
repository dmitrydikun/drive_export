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
	"errors"
	"fmt"
	"github.com/xuri/excelize/v2"
	"google.golang.org/api/drive/v3"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

const (
	originMIME   = "application/vnd.google-apps.spreadsheet"
	exportMIME   = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	exportFormat = "xlsx"
)

type task struct {
	name    string
	taskdir string
	origin  string
	id      string
	source  string
	result  string
	targets map[string]target
	updated bool
}

func newTask(cfg *config, tcfg *taskConfig, expdir string) (*task, error) {
	tdir := filepath.Join(expdir, tcfg.Name)
	if err := os.MkdirAll(tdir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create task %s export dir: %v", tcfg.Name, err)
	}
	targets := make(map[string]target, len(tcfg.Targets))
	for i, tcfg := range tcfg.Targets {
		t, err := newTarget(cfg, tcfg, tdir)
		if err != nil {
			return nil, fmt.Errorf("failed to init target %d: %v", i, err)
		}
		if _, ok := targets[t.ID()]; ok {
			return nil, fmt.Errorf("duplicated target id: %s", t.ID())
		}
		targets[t.ID()] = t
	}
	return &task{
		name:    tcfg.Name,
		taskdir: tdir,
		origin:  tcfg.File,
		source:  filepath.Join(tdir, tcfg.File+"."+exportFormat),
		result:  filepath.Join(tdir, tcfg.File+"_result."+exportFormat),
		targets: targets,
	}, nil
}

func (task *task) fetch(fs *drive.FilesService) error {
	id, err := exportDriveFile(fs, task.origin, originMIME, task.source, exportMIME)
	if err != nil {
		return err
	}
	task.id = id
	return nil
}

func (task *task) process(fs *drive.FilesService) error {
	f, err := excelize.OpenFile(task.source)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.Rows(sheet)
	if err != nil {
		return fmt.Errorf("failed to get rows: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return errors.New("source file empty")
	}
	fields, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to parse field names: %v", err)
	}
	statusColumns := make(map[string]int)
	recordIdColumns := make(map[string]int)
	for i, f := range fields {
		for _, t := range task.targets {
			if f == targetStatusFieldName(t) {
				statusColumns[t.ID()] = i
				continue
			}
			if f == targetRecordIdFieldName(t) {
				recordIdColumns[t.ID()] = i
				continue
			}
		}
	}
	if len(statusColumns) != len(task.targets) {
		return errors.New("invalid source: invalid status columns number")
	}
	if len(recordIdColumns) != len(task.targets) {
		return errors.New("invalid source: invalid record id columns number")
	}

	columnLetter := func(idx int) string {
		return string([]byte{byte('A' + idx)})
	}
	setStatus := func(t target, i int, status string) error {
		if err := f.SetCellValue(sheet, columnLetter(statusColumns[t.ID()])+strconv.Itoa(i), status); err != nil {
			return fmt.Errorf("failed to set target %s status for row %d: %v", t.ID(), i, err)
		}
		return nil
	}
	setRecordId := func(t target, i int, id string) error {
		if err := f.SetCellValue(sheet, columnLetter(recordIdColumns[t.ID()])+strconv.Itoa(i), id); err != nil {
			return fmt.Errorf("failed to set target %s record id for row %d: %v", t.ID(), i, err)
		}
		return nil
	}

	var i = 1
	var total, done, fail int
	for rows.Next() {
		i++
		row, err := rows.Columns()
		if err != nil {
			log.Printf("failed to scan row %d: %v\n", i, err)
			continue
		}
		if len(row) == 0 {
			break
		}

		total++

		var insertTargets, updateTargets []target
		for tid, t := range task.targets {
			statusIdx, recordIdIdx := statusColumns[tid], recordIdColumns[tid]
			var status, recordId string
			if len(row) > statusIdx {
				status = row[statusIdx]
			}
			if len(row) > recordIdIdx {
				recordId = row[recordIdIdx]
			}
			if status == "" && recordId == "" {
				insertTargets = append(insertTargets, t)
				continue
			}
			if status == "" && recordId != "" {
				updateTargets = append(updateTargets, t)
				continue
			}
		}

		if len(insertTargets) == 0 && len(updateTargets) == 0 {
			continue
		}
		rec := make(map[string]string)
		for i, cell := range row {
			rec[fields[i]] = cell
		}

		success := true

		for _, t := range insertTargets {
			status := "ok"
			id, err := t.Insert(rec, fs)
			if err != nil {
				success = false
				status = err.Error()
				log.Printf("failed to proccess target %s for row %d: %v", t.ID(), i, err)
			}
			if err = setStatus(t, i, status); err != nil {
				return err
			}
			if status == "ok" {
				if err = setRecordId(t, i, id); err != nil {
					return err
				}
			}
		}

		//for _, t := range updateTargets {
		//
		//}

		if success {
			done++
		} else {
			fail++
		}
		task.updated = true
	}

	if err = rows.Close(); err != nil {
		log.Printf("failed to close rows: %v", err)
	}

	log.Printf("total: %d; processed: %d; failed: %d\n", total, done, fail)

	if task.updated {
		if err := f.SaveAs(task.result); err != nil {
			return fmt.Errorf("failed to save file: %v", err)
		}
	}
	return err
}

func (task *task) update(fs *drive.FilesService) error {
	if !task.updated {
		return nil
	}

	f, err := os.OpenFile(task.result, os.O_RDONLY, filePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fs.Update(task.id, &drive.File{
		Name:     task.origin,
		MimeType: originMIME,
	}).Media(f).Do()

	if err != nil {
		return fmt.Errorf("upload failed: %v", err)
	}
	return nil
}
