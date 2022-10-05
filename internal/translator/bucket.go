// Copyright 2022 Cockroach Labs Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package translator

import (
	"math"

	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
)

type log10Bucket struct {
	BinNums int
	Curr    float64
	Max     float64
}

func createLog10Bucket(start float64, max float64, bins int) *log10Bucket {
	return &log10Bucket{
		Curr:    start,
		Max:     max,
		BinNums: bins,
	}
}

// Computes the next bin
func (b *log10Bucket) nextBin() {
	c := int(math.Floor(math.Log10(b.Curr)))
	m := math.Pow10(c + 1)
	var n float64
	if b.BinNums < 10 && b.Curr <= math.Pow10(c) {
		n = (m / float64(b.BinNums))
	} else {
		n = b.Curr + (m / float64(b.BinNums))
	}
	if n <= m {
		b.Curr = n
	} else {
		b.Curr = m
	}
}

func (b *log10Bucket) binUpperBound() float64 {
	return b.Curr
}

func (b *log10Bucket) addLog10Buckets(
	currHdrBucket *dto.Bucket, prevHdrBucket *dto.Bucket, newBuckets []*dto.Bucket,
) []*dto.Bucket {
	le := currHdrBucket.GetUpperBound()
	count := currHdrBucket.GetCumulativeCount()
	if le == math.Inf(1) {
		for b.binUpperBound() < b.Max {
			bucket := &dto.Bucket{
				UpperBound:      proto.Float64(b.binUpperBound()),
				CumulativeCount: proto.Uint64(count),
			}
			b.nextBin()
			newBuckets = append(newBuckets, bucket)
		}
		return append(newBuckets, &dto.Bucket{
			UpperBound:      proto.Float64(b.binUpperBound()),
			CumulativeCount: proto.Uint64(count)})

	}
	if prevHdrBucket == nil && b.binUpperBound() < le {
		for b.binUpperBound() < le && b.binUpperBound() <= b.Max {
			b.nextBin()
		}
		return newBuckets
	}
	ple := float64(0)
	pcount := uint64(0)
	if prevHdrBucket != nil {
		ple = prevHdrBucket.GetUpperBound()
		pcount = prevHdrBucket.GetCumulativeCount()
	}
	for b.binUpperBound() < le && b.binUpperBound() <= b.Max {
		// Assuming a uniform distribution within each of the original buckets, adjust the count if the new
		// bucket upper bound falls within the original bucket.
		adj := math.Floor(float64(count-pcount) * (le - b.binUpperBound()) / (le - ple))
		res := count - uint64(adj)
		bucket := &dto.Bucket{
			UpperBound:      proto.Float64(b.binUpperBound()),
			CumulativeCount: proto.Uint64(res),
		}
		b.nextBin()
		newBuckets = append(newBuckets, bucket)
	}
	return newBuckets
}
