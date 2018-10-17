package camelsse

import (
	"time"
)

type CamelSSEData struct {
	EndpointURI string                 `json:"EndpointURI"`
	Headers     CamelSSEDataHeaders    `json:"Headers"`
	Properties  CamelSSEDataProperties `json:"Properties"`
	Body        string                 `json:"Body"`
	ExchangeID  string                 `json:"ExchangeId"`
	TimeStamp   time.Time              `json:"TimeStamp"`
}

type CamelSSEDataHeaders struct {
	Breadcrumbid string          `json:"breadcrumbid"`
	Firedtime    LongTimeWrapper `json:"firedtime"`
}

type CamelSSEDataProperties struct {
	CamelTimer
	ToEndpoint string          `json:"CamelToEndpoint"`
	CreatedAt  LongTimeWrapper `json:"CamelCreatedTimestamp"`
}

type CamelTimer struct {
	FiredTime LongTimeWrapper `json:"CamelTimerFiredTime"`
	Period    string          `json:"CamelTimerPeriod"`
	Name      string          `json:"CamelTimerName"`
	Counter   string          `json:"CamelToEndpoint"`
}

type LongTimeWrapper struct {
	time.Time
}

func (lT *LongTimeWrapper) UnmarshalJSON(bs []byte) error {
	const longForm = "Mon Jan 2 15:04:05 MST 2006"
	st := string(bs)
	t, err := time.Parse(longForm, st[1:len(st)-1])
	if err != nil {
		return err
	}
	lT.Time = t
	return nil
}
