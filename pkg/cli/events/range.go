package events

import (
	"time"

	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/pkg/errors"

	ddcli "github.com/hyperslop-systems/hyperslop-cli/pkg/cli"
	"github.com/hyperslop-systems/hyperslop-cli/pkg/datalab"
)

// The stream-within-a-drop flag is ddcli.DropStreamFlag, shared with the four
// other verbs that carry it. See pkg/cli/fields.go for why it is no longer
// spelled --stream.

// rangeSettings are the query bounds shared by query, tail and export.
type rangeSettings struct {
	Stream    string `glazed:"drop-stream"`
	Limit     int    `glazed:"limit"`
	Order     string `glazed:"order"`
	From      string `glazed:"from"`
	To        string `glazed:"to"`
	After     int64  `glazed:"after"`
	Before    int64  `glazed:"before"`
	TimeField string `glazed:"time-field"`
}

// rangeFields declares the bounds. defaultLimit differs per verb: a query pages
// 50, a tail shows the last 10, an export takes everything it is allowed.
func rangeFields(defaultLimit int) []*fields.Definition {
	return rangeFieldsWithOrder(defaultLimit, string(datalab.OrderDesc))
}

func rangeFieldsWithOrder(defaultLimit int, defaultOrder string) []*fields.Definition {
	return []*fields.Definition{
		ddcli.DropStreamField(),
		fields.New("limit", fields.TypeInteger,
			fields.WithDefault(defaultLimit),
			fields.WithHelp("maximum number of events (server caps at 1000)")),
		fields.New("order", fields.TypeChoice,
			fields.WithChoices("asc", "desc"),
			fields.WithDefault(defaultOrder),
			fields.WithHelp("sequence order")),
		fields.New("from", fields.TypeString,
			fields.WithDefault(""),
			fields.WithHelp("inclusive lower time bound (RFC3339)")),
		fields.New("to", fields.TypeString,
			fields.WithDefault(""),
			fields.WithHelp("exclusive upper time bound (RFC3339)")),
		fields.New("after", fields.TypeInteger,
			fields.WithDefault(0),
			fields.WithHelp("ascending only: return events with sequence greater than this")),
		fields.New("before", fields.TypeInteger,
			fields.WithDefault(0),
			fields.WithHelp("descending only: return events with sequence less than this")),
		fields.New("time-field", fields.TypeChoice,
			fields.WithChoices("time", "received_at"),
			fields.WithDefault("time"),
			fields.WithHelp("timestamp --from/--to filter on")),
	}
}

// query builds an EventQuery, reporting bad flag values before any request is
// made — a usage error must not cost a round trip.
func (s *rangeSettings) query(drop string) (datalab.EventQuery, error) {
	q := datalab.EventQuery{
		Drop:   drop,
		Stream: s.Stream,
		Limit:  s.Limit,
		After:  s.After,
		Before: s.Before,
	}

	order, err := datalab.ParseOrder(s.Order)
	if err != nil {
		return datalab.EventQuery{}, err
	}
	q.Order = order

	timeField, err := datalab.ParseTimeField(s.TimeField)
	if err != nil {
		return datalab.EventQuery{}, err
	}
	q.TimeField = timeField

	for _, bound := range []struct {
		name  string
		value string
		field *time.Time
	}{
		{"--from", s.From, &q.From},
		{"--to", s.To, &q.To},
	} {
		if bound.value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, bound.value)
		if err != nil {
			return datalab.EventQuery{}, errors.Wrapf(err,
				"invalid %s %q: expected an RFC3339 timestamp", bound.name, bound.value)
		}
		*bound.field = parsed.UTC()
	}

	if err := q.Normalize(); err != nil {
		return datalab.EventQuery{}, err
	}
	return q, nil
}
