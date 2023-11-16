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

type item struct {
	name    string
	origin  string
	id      string
	source  string
	result  string
	targets map[string]target
	updated bool
}

func newItem(cfg *config, icfg *itemConfig, dir string) (*item, error) {
	targets := make(map[string]target, len(icfg.Targets))
	for i, tcfg := range icfg.Targets {
		t, err := newTarget(cfg, tcfg, dir)
		if err != nil {
			return nil, fmt.Errorf("failed to init target %d: %v", i, err)
		}
		if _, ok := targets[t.ID()]; ok {
			return nil, fmt.Errorf("duplicated target id: %s", t.ID())
		}
		targets[t.ID()] = t
	}
	return &item{
		name:    icfg.Name,
		origin:  icfg.File,
		source:  filepath.Join(dir, icfg.File+"."+exportFormat),
		result:  filepath.Join(dir, icfg.File+"_result."+exportFormat),
		targets: targets,
	}, nil
}

func (item *item) fetch(fs *drive.FilesService) error {
	id, err := exportDriveFile(fs, item.origin, originMIME, item.source, exportMIME)
	if err != nil {
		return err
	}
	item.id = id
	return nil
}

func (item *item) process(fs *drive.FilesService) error {
	f, err := excelize.OpenFile(item.source)
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
		for _, t := range item.targets {
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
	if len(statusColumns) != len(item.targets) {
		return errors.New("invalid source: invalid status columns number")
	}
	if len(recordIdColumns) != len(item.targets) {
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
		for tid, t := range item.targets {
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
		item.updated = true
	}

	if err = rows.Close(); err != nil {
		log.Printf("failed to close rows: %v", err)
	}

	log.Printf("total: %d; processed: %d; failed: %d\n", total, done, fail)

	if item.updated {
		if err := f.SaveAs(item.result); err != nil {
			return fmt.Errorf("failed to save file: %v", err)
		}
	}
	return err
}

func (item *item) update(fs *drive.FilesService) error {
	if !item.updated {
		return nil
	}

	f, err := os.OpenFile(item.result, os.O_RDONLY, filePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fs.Update(item.id, &drive.File{
		Name:     item.origin,
		MimeType: originMIME,
	}).Media(f).Do()

	if err != nil {
		return fmt.Errorf("upload failed: %v", err)
	}
	return nil
}
