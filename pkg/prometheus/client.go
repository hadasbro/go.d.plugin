// SPDX-License-Identifier: GPL-3.0-or-later

package prometheus

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"

	"github.com/netdata/go.d.plugin/pkg/prometheus/selector"
	"github.com/netdata/go.d.plugin/pkg/web"
)

type (
	// Prometheus is a helper for scrape and parse prometheus format metrics.
	Prometheus interface {
		// ScrapeSeries and parse prometheus format metrics
		ScrapeSeries() (Series, error)
		Scrape() (MetricFamilies, error)
	}

	prometheus struct {
		client  *http.Client
		request web.Request

		sr selector.Selector

		parser promTextParser

		buf     *bytes.Buffer
		gzipr   *gzip.Reader
		bodyBuf *bufio.Reader
	}
)

const (
	acceptHeader    = `text/plain;version=0.0.4;q=1,*/*;q=0.1`
	userAgentHeader = `netdata/go.d.plugin`
)

// New creates a Prometheus instance.
func New(client *http.Client, request web.Request) Prometheus {
	return &prometheus{
		client:  client,
		request: request,
		buf:     bytes.NewBuffer(make([]byte, 0, 16000)),
	}
}

// NewWithSelector creates a Prometheus instance with the selector.
func NewWithSelector(client *http.Client, request web.Request, sr selector.Selector) Prometheus {
	return &prometheus{
		client:  client,
		request: request,
		sr:      sr,
		buf:     bytes.NewBuffer(make([]byte, 0, 16000)),
		parser:  promTextParser{sr: sr},
	}
}

// ScrapeSeries scrapes metrics, parses and sorts
func (p *prometheus) ScrapeSeries() (Series, error) {
	p.buf.Reset()

	if err := p.fetch(p.buf); err != nil {
		return nil, err
	}

	return p.parser.parseToSeries(p.buf.Bytes())
}

func (p *prometheus) Scrape() (MetricFamilies, error) {
	p.buf.Reset()

	if err := p.fetch(p.buf); err != nil {
		return nil, err
	}

	return p.parser.parseToMetricFamilies(p.buf.Bytes())
}

func (p *prometheus) fetch(w io.Writer) error {
	req, err := web.NewHTTPRequest(p.request)
	if err != nil {
		return err
	}

	req.Header.Add("Accept", acceptHeader)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgentHeader)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server '%s' returned HTTP status code %d (%s)", req.URL, resp.StatusCode, resp.Status)
	}

	if resp.Header.Get("Content-Encoding") != "gzip" {
		_, err = io.Copy(w, resp.Body)
		return err
	}

	if p.gzipr == nil {
		p.bodyBuf = bufio.NewReader(resp.Body)
		p.gzipr, err = gzip.NewReader(p.bodyBuf)
		if err != nil {
			return err
		}
	} else {
		p.bodyBuf.Reset(resp.Body)
		_ = p.gzipr.Reset(p.bodyBuf)
	}

	_, err = io.Copy(w, p.gzipr)
	_ = p.gzipr.Close()

	return err
}
