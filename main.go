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
	"flag"
	"fmt"
	"log"
)

var (
	flagNoClean = flag.Bool("no-clean", false, "do not remove fetched/modified files on exit")
	flagBotMode = flag.Bool("bot-mode", false, "listen bot events")
)

func main() {
	flag.Parse()

	cfg, err := readConfig()
	if err != nil {
		log.Fatalf("failed to read config: %v", err)
	}

	runExport := func() ([]taskResult, error) {
		exp, err := newExport(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed init export: %v", err)
		}
		exp.fetch()
		results := exp.process()
		exp.upload()
		if !*flagNoClean {
			exp.clean()
		}
		return results, nil
	}

	if *flagBotMode {
		err = telegramListenBot(cfg, runExport)
	} else {
		_, err = runExport()
	}

	if err != nil {
		log.Fatal(err)
	}
}
