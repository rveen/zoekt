// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"flag"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"

	svn "github.com/rveen/goapi/svnapi"
	"github.com/rveen/zoekt/build"
	"github.com/sajari/docconv"
)

func main() {
	var cpuProfile = flag.String("cpu_profile", "", "write cpu profile to file")
	var sizeMax = flag.Int("file_limit", 128*1024, "maximum file size")
	var shardLimit = flag.Int("shard_limit", 100<<20, "maximum corpus size for a shard")
	var parallelism = flag.Int("parallelism", 4, "maximum number of parallel indexing processes.")

	ignoreDirs := flag.String("ignore_dirs", ".git,.hg,.svn", "comma separated list of directories to ignore.")
	indexDir := flag.String("index", build.DefaultDir, "directory for search indices")
	flag.Parse()

	opts := build.Options{
		Parallelism: *parallelism,
		SizeMax:     *sizeMax,
		ShardMax:    *shardLimit,
		IndexDir:    *indexDir,
	}
	opts.SetDefaults()

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	ignoreDirMap := map[string]struct{}{}
	if *ignoreDirs != "" {
		dirs := strings.Split(*ignoreDirs, ",")
		for _, d := range dirs {
			d = strings.TrimSpace(d)
			if d != "" {
				ignoreDirMap[d] = struct{}{}
			}
		}
	}

	for _, arg := range flag.Args() {
		if err := indexArg(arg, opts, ignoreDirMap); err != nil {
			log.Fatal(err)
		}
	}
}

func indexArg(arg string, opts build.Options, ignore map[string]struct{}) error {
	dir, err := filepath.Abs(filepath.Clean(arg))
	if err != nil {
		return err
	}

	opts.RepositoryDescription.Name = filepath.Base(dir)

	builder, err := build.NewBuilder(opts)
	if err != nil {
		return err
	}

	comm := make(chan string, 100)

	go func() {
		traverseDir(dir, comm)
		close(comm)

	}()

	for f := range comm {
		log.Println("file", f)
		var content []byte

		content = svn.File(f, -1)
		mime := docconv.MimeTypeByExtension(f)

		if strings.HasSuffix(f, ".pdf") {

			r := bytes.NewReader(content)
			res, err := docconv.Convert(r, mime, false)
			if err != nil {
				log.Println(" - PDF: error")
				continue
			}
			content = []byte(res.Body)
			log.Println(" - PDF: content length", len(content))
		}

		f = strings.TrimPrefix(f, dir+"/")
		builder.AddFile(f, content)
	}

	return builder.Finish()
}

func traverseDir(dir string, comm chan string) {

	d, err := svn.List(dir, -1)
	if err != nil {
		return
	}
	for _, e := range d.Out {

		name := e.Get("name").String()

		switch e.Get("kind").String() {
		case "file":
			comm <- dir + "/" + name
		case "dir":
			traverseDir(dir+"/"+name, comm)
		}
	}
}
