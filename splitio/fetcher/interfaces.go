// Package fetcher implements all kind of Split/Segments fetchers
package fetcher

import "github.com/splitio/go-agent/splitio/api"

// SplitFetcher interface to be implemented by Split Fetchers
type SplitFetcher interface {
	Fetch() ([]api.SplitDTO, error)
}

// SegmentFetcher interface to be implemented by Segment Fetchers
type SegmentFetcher interface {
	Fetch(name string, since int64) (api.SegmentChangesDTO, error)
}
