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
	"flag"
	"log"
	"net"
	"time"

	"golang.org/x/net/context"

	"github.com/google/zoekt"
	"github.com/google/zoekt/build"
	"github.com/google/zoekt/query"
	"github.com/google/zoekt/shards"
	"github.com/rveen/ogdl"
	rf "github.com/rveen/ogdl/ogdlrf"
)

const defaultNumResults = 50

var searcher zoekt.Searcher

func main() {

	var err error

	index := flag.String("index", build.DefaultDir, "set index directory to use")
	flag.Parse()

	searcher, err = shards.NewDirectorySearcher(*index)
	if err != nil {
		log.Fatal(err)
	}

	srv := rf.Server{Host: ":1166", Timeout: 10}
	srv.AddRoute("search", search)
	srv.Serve()
}

func search(c net.Conn, g *ogdl.Graph) *ogdl.Graph {

	q := g.GetAt(0).GetAt(0).ThisString()
	repo := g.GetAt(0).GetAt(1).ThisString()

	re, _ := Search(q, 50, repo)

	return re
}

func Search(qs string, num int, repo string) (*ogdl.Graph, error) {

	q, err := query.Parse(qs)
	if err != nil {
		return nil, err
	}

	repoOnly := true
	query.VisitAtoms(q, func(q query.Q) {
		_, ok := q.(*query.Repo)
		repoOnly = repoOnly && ok
	})

	sOpts := zoekt.SearchOptions{
		MaxWallTime: 10 * time.Second,
	}

	sOpts.SetDefaults()

	sOpts.Repo = repo

	ctx := context.Background()
	if result, err := searcher.Search(ctx, q, &zoekt.SearchOptions{EstimateDocCount: true, Repo: repo}); err != nil {
		return nil, err
	} else if numdocs := result.ShardFilesConsidered; numdocs > 10000 {
		// If the search touches many shards and many files, we
		// have to limit the number of matches.  This setting
		// is based on the number of documents eligible after
		// considering reponames, so large repos (both
		// android, chromium are about 500k files) aren't
		// covered fairly.

		// 10k docs, 50 num -> max match = (250 + 250 / 10)
		sOpts.ShardMaxMatchCount = num*5 + (5*num)/(numdocs/1000)

		// 10k docs, 50 num -> max important match = 4
		sOpts.ShardMaxImportantMatch = num/20 + num/(numdocs/500)
	} else {
		// Virtually no limits for a small corpus; important
		// matches are just as expensive as normal matches.
		n := numdocs + num*100
		sOpts.ShardMaxImportantMatch = n
		sOpts.ShardMaxMatchCount = n
		sOpts.TotalMaxMatchCount = n
		sOpts.TotalMaxImportantMatch = n
	}

	result, err := searcher.Search(ctx, q, &sOpts)
	if err != nil {
		return nil, err
	}

	if len(result.Files) > num {
		result.Files = result.Files[:num]
	}

	g := ogdl.New()

	for _, f := range result.Files {
		n := g.Add("_")
		n.Add("file").Add(f.FileName)
		n.Add("repo").Add(f.Repository)
		ll := n.Add("lines")
		for _, l := range f.LineMatches {
			ll.Add(l.Line)
		}
	}

	return g, nil
}
