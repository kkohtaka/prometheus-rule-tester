// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tsdb

import (
	"time"
	"unsafe"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/tsdb"
	tsdbLabels "github.com/prometheus/tsdb/labels"
)

func Adapter(db *tsdb.DB) storage.Storage {
	return &adapter{db: db}
}

// adapter implements a storage.Storage around TSDB.
type adapter struct {
	db *tsdb.DB
}

// Options of the DB storage.
type Options struct {
	// The interval at which the write ahead log is flushed to disc.
	WALFlushInterval time.Duration

	// The timestamp range of head blocks after which they get persisted.
	// It's the minimum duration of any persisted block.
	MinBlockDuration model.Duration

	// The maximum timestamp range of compacted blocks.
	MaxBlockDuration model.Duration

	// Duration for how long to retain data.
	Retention model.Duration

	// Disable creation and consideration of lockfile.
	NoLockfile bool
}

// Open returns a new storage backed by a TSDB database that is configured for Prometheus.
func Open(path string, r prometheus.Registerer, opts *Options) (*tsdb.DB, error) {
	// Start with smallest block duration and create exponential buckets until the exceed the
	// configured maximum block duration.
	rngs := tsdb.ExponentialBlockRanges(int64(time.Duration(opts.MinBlockDuration).Seconds()*1000), 3, 10)

	for i, v := range rngs {
		if v > int64(time.Duration(opts.MaxBlockDuration).Seconds()*1000) {
			rngs = rngs[:i]
			break
		}
	}

	db, err := tsdb.Open(path, nil, r, &tsdb.Options{
		WALFlushInterval:  10 * time.Second,
		RetentionDuration: uint64(time.Duration(opts.Retention).Seconds() * 1000),
		BlockRanges:       rngs,
		NoLockfile:        opts.NoLockfile,
	})
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (a adapter) Querier(mint, maxt int64) (storage.Querier, error) {
	return querier{q: a.db.Querier(mint, maxt)}, nil
}

// Appender returns a new appender against the storage.
func (a adapter) Appender() (storage.Appender, error) {
	return appender{a: a.db.Appender()}, nil
}

// Close closes the storage and all its underlying resources.
func (a adapter) Close() error {
	return a.db.Close()
}

type querier struct {
	q tsdb.Querier
}

func (q querier) Select(oms ...*labels.Matcher) storage.SeriesSet {
	ms := make([]tsdbLabels.Matcher, 0, len(oms))

	for _, om := range oms {
		ms = append(ms, convertMatcher(om))
	}

	return seriesSet{set: q.q.Select(ms...)}
}

func (q querier) LabelValues(name string) ([]string, error) { return q.q.LabelValues(name) }
func (q querier) Close() error                              { return q.q.Close() }

type seriesSet struct {
	set tsdb.SeriesSet
}

func (s seriesSet) Next() bool         { return s.set.Next() }
func (s seriesSet) Err() error         { return s.set.Err() }
func (s seriesSet) At() storage.Series { return series{s: s.set.At()} }

type series struct {
	s tsdb.Series
}

func (s series) Labels() labels.Labels            { return toLabels(s.s.Labels()) }
func (s series) Iterator() storage.SeriesIterator { return storage.SeriesIterator(s.s.Iterator()) }

type appender struct {
	a tsdb.Appender
}

func (a appender) Add(lset labels.Labels, t int64, v float64) (string, error) {
	ref, err := a.a.Add(toTSDBLabels(lset), t, v)

	switch errors.Cause(err) {
	case tsdb.ErrNotFound:
		return "", storage.ErrNotFound
	case tsdb.ErrOutOfOrderSample:
		return "", storage.ErrOutOfOrderSample
	case tsdb.ErrAmendSample:
		return "", storage.ErrDuplicateSampleForTimestamp
	case tsdb.ErrOutOfBounds:
		return "", storage.ErrOutOfBounds
	}
	return ref, err
}

func (a appender) AddFast(_ labels.Labels, ref string, t int64, v float64) error {
	err := a.a.AddFast(ref, t, v)

	switch errors.Cause(err) {
	case tsdb.ErrNotFound:
		return storage.ErrNotFound
	case tsdb.ErrOutOfOrderSample:
		return storage.ErrOutOfOrderSample
	case tsdb.ErrAmendSample:
		return storage.ErrDuplicateSampleForTimestamp
	case tsdb.ErrOutOfBounds:
		return storage.ErrOutOfBounds
	}
	return err
}

func (a appender) Commit() error   { return a.a.Commit() }
func (a appender) Rollback() error { return a.a.Rollback() }

func convertMatcher(m *labels.Matcher) tsdbLabels.Matcher {
	switch m.Type {
	case labels.MatchEqual:
		return tsdbLabels.NewEqualMatcher(m.Name, m.Value)

	case labels.MatchNotEqual:
		return tsdbLabels.Not(tsdbLabels.NewEqualMatcher(m.Name, m.Value))

	case labels.MatchRegexp:
		res, err := tsdbLabels.NewRegexpMatcher(m.Name, m.Value)
		if err != nil {
			panic(err)
		}
		return res

	case labels.MatchNotRegexp:
		res, err := tsdbLabels.NewRegexpMatcher(m.Name, m.Value)
		if err != nil {
			panic(err)
		}
		return tsdbLabels.Not(res)
	}
	panic("storage.convertMatcher: invalid matcher type")
}

func toTSDBLabels(l labels.Labels) tsdbLabels.Labels {
	return *(*tsdbLabels.Labels)(unsafe.Pointer(&l))
}

func toLabels(l tsdbLabels.Labels) labels.Labels {
	return *(*labels.Labels)(unsafe.Pointer(&l))
}
