package xray

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
)

// rawCodec passes pre-encoded protobuf bytes through gRPC, so we can talk to
// Xray's StatsService without importing Xray's generated code.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, fmt.Errorf("rawCodec: unexpected %T", v)
	}
	return *b, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	b, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawCodec: unexpected %T", v)
	}
	*b = append((*b)[:0], data...)
	return nil
}

func (rawCodec) Name() string { return "proto" }

// Stat is one counter returned by QueryStats.
type Stat struct {
	Name  string
	Value int64
}

// StatsClient queries xray.app.stats.command.StatsService.
type StatsClient struct {
	conn *grpc.ClientConn
}

// NewStatsClient connects to the Xray API at addr (host:port).
func NewStatsClient(addr string) (*StatsClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &StatsClient{conn: conn}, nil
}

// Close closes the connection.
func (c *StatsClient) Close() error { return c.conn.Close() }

// EncodeQueryStatsRequest encodes QueryStatsRequest{pattern=1, reset=2}.
func EncodeQueryStatsRequest(pattern string, reset bool) []byte {
	var b []byte
	if pattern != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, pattern)
	}
	if reset {
		b = protowire.AppendTag(b, 2, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	return b
}

// DecodeQueryStatsResponse decodes QueryStatsResponse{repeated Stat stat=1}.
func DecodeQueryStatsResponse(b []byte) ([]Stat, error) {
	var out []Stat
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			msg, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return nil, protowire.ParseError(m)
			}
			b = b[m:]
			st, err := decodeStat(msg)
			if err != nil {
				return nil, err
			}
			out = append(out, st)
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return nil, protowire.ParseError(m)
		}
		b = b[m:]
	}
	return out, nil
}

func decodeStat(b []byte) (Stat, error) {
	var s Stat
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return s, protowire.ParseError(n)
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.BytesType:
			v, m := protowire.ConsumeString(b)
			if m < 0 {
				return s, protowire.ParseError(m)
			}
			s.Name, b = v, b[m:]
		case num == 2 && typ == protowire.VarintType:
			v, m := protowire.ConsumeVarint(b)
			if m < 0 {
				return s, protowire.ParseError(m)
			}
			s.Value, b = int64(v), b[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				return s, protowire.ParseError(m)
			}
			b = b[m:]
		}
	}
	return s, nil
}

// Query calls QueryStats.
func (c *StatsClient) Query(ctx context.Context, pattern string, reset bool) ([]Stat, error) {
	req := EncodeQueryStatsRequest(pattern, reset)
	var resp []byte
	err := c.conn.Invoke(ctx, "/xray.app.stats.command.StatsService/QueryStats", &req, &resp, grpc.ForceCodec(rawCodec{}))
	if err != nil {
		return nil, err
	}
	return DecodeQueryStatsResponse(resp)
}

// Traffic is aggregated traffic deltas from one poll.
type Traffic struct {
	Users   map[int64][2]int64 // id -> {up, down}
	SysUp   int64
	SysDown int64
}

// ParseStats splits stat counters into per-user and system (inbound) traffic.
// Names look like "user>>>EMAIL>>>traffic>>>uplink" and
// "inbound>>>TAG>>>traffic>>>downlink".
func ParseStats(stats []Stat) Traffic {
	t := Traffic{Users: map[int64][2]int64{}}
	for _, s := range stats {
		parts := strings.Split(s.Name, ">>>")
		if len(parts) != 4 || parts[2] != "traffic" {
			continue
		}
		up := parts[3] == "uplink"
		switch parts[0] {
		case "user":
			id, ok := ParseEmail(parts[1])
			if !ok {
				continue
			}
			v := t.Users[id]
			if up {
				v[0] += s.Value
			} else {
				v[1] += s.Value
			}
			t.Users[id] = v
		case "inbound":
			if parts[1] == "api" {
				continue
			}
			if up {
				t.SysUp += s.Value
			} else {
				t.SysDown += s.Value
			}
		}
	}
	return t
}
