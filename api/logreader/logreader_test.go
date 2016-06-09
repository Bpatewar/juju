// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package logreader_test

import (
	"net/url"
	"time"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/api/base"
	basetesting "github.com/juju/juju/api/base/testing"
	"github.com/juju/juju/api/logreader"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/logfwd"
	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/version"
)

type LogsReaderSuite struct {
	coretesting.BaseSuite
}

var _ = gc.Suite(&LogsReaderSuite{})

func (s *LogsReaderSuite) TestLogReader(c *gc.C) {
	ts := time.Now()
	apiRec := params.LogRecord{
		ControllerUUID: "9f484882-2f18-4fd2-967d-db9663db7bea",
		ModelUUID:      "deadbeef-2f18-4fd2-967d-db9663db7bea",
		Time:           ts,
		Module:         "api.logreader.test",
		Location:       "test.go:42",
		Level:          loggo.INFO,
		Message:        "test message",
	}
	stub := &testing.Stub{}
	stream := mockStream{stub: stub}
	stream.ReturnReadJSON = apiRec
	conn := &mockConnector{stub: stub}
	conn.ReturnConnectStream = stream
	a := logreader.NewAPI(conn)
	r, err := a.LogsReader(time.Time{})
	c.Assert(err, gc.IsNil)

	channel := r.ReadLogs()
	c.Assert(channel, gc.NotNil)

	stub.CheckCall(c, 0, "ConnectStream", `/log`, url.Values{
		"format": []string{"json"},
		"all":    []string{"true"},
	})
	select {
	case logRecord := <-channel:
		c.Check(logRecord, jc.DeepEquals, logfwd.Record{
			Origin: logfwd.Origin{
				ControllerUUID: "9f484882-2f18-4fd2-967d-db9663db7bea",
				ModelUUID:      "deadbeef-2f18-4fd2-967d-db9663db7bea",
				JujuVersion:    version.Current,
			},
			Timestamp: ts,
			Level:     loggo.INFO,
			Location: logfwd.SourceLocation{
				Module:   "api.logreader.test",
				Filename: "test.go",
				Line:     42,
			},
			Message: "test message",
		})
	case <-time.After(coretesting.LongWait):
		c.Errorf("timed out waiting for kill")
	}

	r.Kill()
	c.Assert(r.Wait(), jc.ErrorIsNil)

	stub.CheckCallNames(c, "ConnectStream", "ReadJSON", "ReadJSON", "Close")
}

func (s *LogsReaderSuite) TestNewAPIReadLogError(c *gc.C) {
	stub := &testing.Stub{}
	conn := &mockConnector{stub: stub}
	failure := errors.New("foo")
	stub.SetErrors(failure)
	a := logreader.NewAPI(conn)

	_, err := a.LogsReader(time.Time{})

	stub.CheckCallNames(c, "ConnectStream")
	c.Check(err, gc.ErrorMatches, "cannot connect to /log: foo")
}

func (s *LogsReaderSuite) TestNewAPIWriteError(c *gc.C) {
	stub := &testing.Stub{}
	stream := mockStream{stub: stub}
	conn := &mockConnector{stub: stub}
	conn.ReturnConnectStream = stream
	failure := errors.New("an error")
	stub.SetErrors(nil, failure)
	a := logreader.NewAPI(conn)

	r, err := a.LogsReader(time.Time{})
	c.Assert(err, gc.IsNil)

	channel := r.ReadLogs()
	c.Assert(channel, gc.NotNil)

	select {
	case <-channel:
		c.Assert(r.Wait(), gc.ErrorMatches, "an error")
	case <-time.After(coretesting.LongWait):
		c.Fail()
	}
	stub.CheckCallNames(c, "ConnectStream", "ReadJSON", "Close")
}

type mockConnector struct {
	basetesting.APICallerFunc
	stub *testing.Stub

	ReturnConnectStream base.Stream
}

func (c *mockConnector) ConnectStream(path string, values url.Values) (base.Stream, error) {
	c.stub.AddCall("ConnectStream", path, values)
	if err := c.stub.NextErr(); err != nil {
		return nil, errors.Trace(err)
	}
	return c.ReturnConnectStream, nil
}

type mockStream struct {
	base.Stream
	stub *testing.Stub

	ReturnReadJSON params.LogRecord
}

func (s mockStream) ReadJSON(v interface{}) error {
	s.stub.AddCall("ReadJSON", v)
	if err := s.stub.NextErr(); err != nil {
		return errors.Trace(err)
	}

	switch vt := v.(type) {
	case *params.LogRecord:
		*vt = s.ReturnReadJSON
		return nil
	default:
		return errors.Errorf("unexpected output type: %T", v)
	}
}

func (s mockStream) Close() error {
	s.stub.AddCall("Close")
	if err := s.stub.NextErr(); err != nil {
		return errors.Trace(err)
	}
	return nil
}
