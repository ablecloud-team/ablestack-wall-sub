package cloudmonitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/grafana/grafana/pkg/infra/httpclient"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/plugins"
	"github.com/grafana/grafana/pkg/plugins/backendplugin"
	"github.com/grafana/grafana/pkg/plugins/backendplugin/coreplugin"
	"github.com/grafana/grafana/pkg/services/datasources"

	"github.com/grafana/grafana/pkg/setting"
)

var (
	slog = log.New("tsdb.cloudMonitoring")
)

var (
	matchAllCap                 = regexp.MustCompile("(.)([A-Z][a-z]*)")
	legendKeyFormat             = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)
	metricNameFormat            = regexp.MustCompile(`([\w\d_]+)\.(googleapis\.com|io)/(.+)`)
	wildcardRegexRe             = regexp.MustCompile(`[-\/^$+?.()|[\]{}]`)
	alignmentPeriodRe           = regexp.MustCompile("[0-9]+")
	cloudMonitoringUnitMappings = map[string]string{
		"bit":     "bits",
		"By":      "bytes",
		"s":       "s",
		"min":     "m",
		"h":       "h",
		"d":       "d",
		"us":      "µs",
		"ms":      "ms",
		"ns":      "ns",
		"%":       "percent",
		"percent": "percent",
		"MiBy":    "mbytes",
		"By/s":    "Bps",
		"GBy":     "decgbytes",
	}
)

const (
	dsName = "stackdriver"

	gceAuthentication         string = "gce"
	jwtAuthentication         string = "jwt"
	metricQueryType           string = "metrics"
	sloQueryType              string = "slo"
	mqlEditorMode             string = "mql"
	crossSeriesReducerDefault string = "REDUCE_NONE"
	perSeriesAlignerDefault   string = "ALIGN_MEAN"
)

func ProvideService(cfg *setting.Cfg, httpClientProvider httpclient.Provider, pluginManager plugins.Manager,
	backendPluginManager backendplugin.Manager, dsService *datasources.Service) *Service {
	s := &Service{
		pluginManager:        pluginManager,
		backendPluginManager: backendPluginManager,
		httpClientProvider:   httpClientProvider,
		cfg:                  cfg,
		im:                   datasource.NewInstanceManager(newInstanceSettings(httpClientProvider)),
		dsService:            dsService,
	}

	factory := coreplugin.New(backend.ServeOpts{
		QueryDataHandler: s,
	})

	if err := s.backendPluginManager.Register(dsName, factory); err != nil {
		slog.Error("Failed to register plugin", "error", err)
	}
	return s
}

type Service struct {
	pluginManager        plugins.Manager
	backendPluginManager backendplugin.Manager
	httpClientProvider   httpclient.Provider
	cfg                  *setting.Cfg
	im                   instancemgmt.InstanceManager
	dsService            *datasources.Service
}

type QueryModel struct {
	Type string `json:"type"`
}

type datasourceInfo struct {
	id                 int64
	updated            time.Time
	url                string
	authenticationType string
	defaultProject     string
	clientEmail        string
	tokenUri           string
	client             *http.Client

	decryptedSecureJSONData map[string]string
}

func newInstanceSettings(httpClientProvider httpclient.Provider) datasource.InstanceFactoryFunc {
	return func(settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
		var jsonData map[string]interface{}
		err := json.Unmarshal(settings.JSONData, &jsonData)
		if err != nil {
			return nil, fmt.Errorf("error reading settings: %w", err)
		}

		authType := jwtAuthentication
		if authTypeOverride, ok := jsonData["authenticationType"].(string); ok && authTypeOverride != "" {
			authType = authTypeOverride
		}

		var defaultProject string
		if jsonData["defaultProject"] != nil {
			defaultProject = jsonData["defaultProject"].(string)
		}

		var clientEmail string
		if jsonData["clientEmail"] != nil {
			clientEmail = jsonData["clientEmail"].(string)
		}

		var tokenUri string
		if jsonData["tokenUri"] != nil {
			tokenUri = jsonData["tokenUri"].(string)
		}

		dsInfo := &datasourceInfo{
			id:                      settings.ID,
			updated:                 settings.Updated,
			url:                     settings.URL,
			authenticationType:      authType,
			defaultProject:          defaultProject,
			clientEmail:             clientEmail,
			tokenUri:                tokenUri,
			decryptedSecureJSONData: settings.DecryptedSecureJSONData,
		}

		opts, err := settings.HTTPClientOptions()
		if err != nil {
			return nil, err
		}

		dsInfo.client, err = newHTTPClient(dsInfo, opts, httpClientProvider)
		if err != nil {
			return nil, err
		}

		return dsInfo, nil
	}
}

// Query takes in the frontend queries, parses them into the CloudMonitoring query format
// executes the queries against the CloudMonitoring API and parses the response into
// the data frames
func (s *Service) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	resp := backend.NewQueryDataResponse()
	if len(req.Queries) == 0 {
		return resp, fmt.Errorf("query contains no queries")
	}

	model := &QueryModel{}
	err := json.Unmarshal(req.Queries[0].JSON, model)
	if err != nil {
		return resp, err
	}

	dsInfo, err := s.getDSInfo(req.PluginContext)
	if err != nil {
		return nil, err
	}

	switch model.Type {
	case "annotationQuery":
		resp, err = s.executeAnnotationQuery(ctx, req, *dsInfo)
	case "getGCEDefaultProject":
		resp, err = s.getGCEDefaultProject(ctx, req, *dsInfo)
	case "timeSeriesQuery":
		fallthrough
	default:
		resp, err = s.executeTimeSeriesQuery(ctx, req, *dsInfo)
	}

	return resp, err
}

func (s *Service) getGCEDefaultProject(ctx context.Context, req *backend.QueryDataRequest, dsInfo datasourceInfo) (*backend.QueryDataResponse, error) {
	gceDefaultProject, err := s.getDefaultProject(ctx, dsInfo)
	if err != nil {
		return backend.NewQueryDataResponse(), fmt.Errorf(
			"failed to retrieve default project from GCE metadata server, error: %w", err)
	}

	return &backend.QueryDataResponse{
		Responses: backend.Responses{
			req.Queries[0].RefID: {
				Frames: data.Frames{data.NewFrame("").SetMeta(&data.FrameMeta{
					Custom: map[string]interface{}{
						"defaultProject": gceDefaultProject,
					},
				})},
			},
		},
	}, nil
}

func (s *Service) executeTimeSeriesQuery(ctx context.Context, req *backend.QueryDataRequest, dsInfo datasourceInfo) (
	*backend.QueryDataResponse, error) {
	resp := backend.NewQueryDataResponse()
	queryExecutors, err := s.buildQueryExecutors(req)
	if err != nil {
		return resp, err
	}

	for _, queryExecutor := range queryExecutors {
		queryRes, dr, executedQueryString, err := queryExecutor.run(ctx, req, s, dsInfo)
		if err != nil {
			return resp, err
		}
		err = queryExecutor.parseResponse(queryRes, dr, executedQueryString)
		if err != nil {
			queryRes.Error = err
		}

		resp.Responses[queryExecutor.getRefID()] = *queryRes
	}

	return resp, nil
}

func queryModel(query backend.DataQuery) (grafanaQuery, error) {
	var rawQuery map[string]interface{}
	err := json.Unmarshal(query.JSON, &rawQuery)
	if err != nil {
		return grafanaQuery{}, err
	}

	if rawQuery["metricQuery"] == nil {
		// migrate legacy query
		var mq metricQuery
		err = json.Unmarshal(query.JSON, &mq)
		if err != nil {
			return grafanaQuery{}, err
		}

		return grafanaQuery{
			QueryType:   metricQueryType,
			MetricQuery: mq,
		}, nil
	}

	var q grafanaQuery
	err = json.Unmarshal(query.JSON, &q)
	if err != nil {
		return grafanaQuery{}, err
	}

	return q, nil
}

func (s *Service) buildQueryExecutors(req *backend.QueryDataRequest) ([]cloudMonitoringQueryExecutor, error) {
	var cloudMonitoringQueryExecutors []cloudMonitoringQueryExecutor
	startTime := req.Queries[0].TimeRange.From
	endTime := req.Queries[0].TimeRange.To
	durationSeconds := int(endTime.Sub(startTime).Seconds())

	for _, query := range req.Queries {
		q, err := queryModel(query)
		if err != nil {
			return nil, fmt.Errorf("could not unmarshal CloudMonitoringQuery json: %w", err)
		}

		q.MetricQuery.PreprocessorType = toPreprocessorType(q.MetricQuery.Preprocessor)
		var target string
		params := url.Values{}
		params.Add("interval.startTime", startTime.UTC().Format(time.RFC3339))
		params.Add("interval.endTime", endTime.UTC().Format(time.RFC3339))

		var queryInterface cloudMonitoringQueryExecutor
		cmtsf := &cloudMonitoringTimeSeriesFilter{
			RefID:    query.RefID,
			GroupBys: []string{},
		}
		switch q.QueryType {
		case metricQueryType:
			if q.MetricQuery.EditorMode == mqlEditorMode {
				queryInterface = &cloudMonitoringTimeSeriesQuery{
					RefID:       query.RefID,
					ProjectName: q.MetricQuery.ProjectName,
					Query:       q.MetricQuery.Query,
					IntervalMS:  query.Interval.Milliseconds(),
					AliasBy:     q.MetricQuery.AliasBy,
					timeRange:   req.Queries[0].TimeRange,
				}
			} else {
				cmtsf.AliasBy = q.MetricQuery.AliasBy
				cmtsf.ProjectName = q.MetricQuery.ProjectName
				cmtsf.GroupBys = append(cmtsf.GroupBys, q.MetricQuery.GroupBys...)
				if q.MetricQuery.View == "" {
					q.MetricQuery.View = "FULL"
				}
				params.Add("filter", buildFilterString(q.MetricQuery.MetricType, q.MetricQuery.Filters))
				params.Add("view", q.MetricQuery.View)
				setMetricAggParams(&params, &q.MetricQuery, durationSeconds, query.Interval.Milliseconds())
				queryInterface = cmtsf
			}
		case sloQueryType:
			cmtsf.AliasBy = q.SloQuery.AliasBy
			cmtsf.ProjectName = q.SloQuery.ProjectName
			cmtsf.Selector = q.SloQuery.SelectorName
			cmtsf.Service = q.SloQuery.ServiceId
			cmtsf.Slo = q.SloQuery.SloId
			params.Add("filter", buildSLOFilterExpression(q.SloQuery))
			setSloAggParams(&params, &q.SloQuery, durationSeconds, query.Interval.Milliseconds())
			queryInterface = cmtsf
		default:
			panic(fmt.Sprintf("Unrecognized query type %q", q.QueryType))
		}

		target = params.Encode()
		cmtsf.Target = target
		cmtsf.Params = params

		if setting.Env == setting.Dev {
			slog.Debug("CloudMonitoring request", "params", params)
		}

		cloudMonitoringQueryExecutors = append(cloudMonitoringQueryExecutors, queryInterface)
	}

	return cloudMonitoringQueryExecutors, nil
}

func interpolateFilterWildcards(value string) string {
	matches := strings.Count(value, "*")
	switch {
	case matches == 2 && strings.HasSuffix(value, "*") && strings.HasPrefix(value, "*"):
		value = strings.ReplaceAll(value, "*", "")
		value = fmt.Sprintf(`has_substring("%s")`, value)
	case matches == 1 && strings.HasPrefix(value, "*"):
		value = strings.Replace(value, "*", "", 1)
		value = fmt.Sprintf(`ends_with("%s")`, value)
	case matches == 1 && strings.HasSuffix(value, "*"):
		value = reverse(strings.Replace(reverse(value), "*", "", 1))
		value = fmt.Sprintf(`starts_with("%s")`, value)
	case matches != 0:
		value = string(wildcardRegexRe.ReplaceAllFunc([]byte(value), func(in []byte) []byte {
			return []byte(strings.Replace(string(in), string(in), `\\`+string(in), 1))
		}))
		value = strings.ReplaceAll(value, "*", ".*")
		value = strings.ReplaceAll(value, `"`, `\\"`)
		value = fmt.Sprintf(`monitoring.regex.full_match("^%s$")`, value)
	}

	return value
}

func buildFilterString(metricType string, filterParts []string) string {
	filterString := ""
	for i, part := range filterParts {
		mod := i % 4
		switch {
		case part == "AND":
			filterString += " "
		case mod == 2:
			operator := filterParts[i-1]
			switch {
			case operator == "=~" || operator == "!=~":
				filterString = reverse(strings.Replace(reverse(filterString), "~", "", 1))
				filterString += fmt.Sprintf(`monitoring.regex.full_match("%s")`, part)
			case strings.Contains(part, "*"):
				filterString += interpolateFilterWildcards(part)
			default:
				filterString += fmt.Sprintf(`"%s"`, part)
			}
		default:
			filterString += part
		}
	}

	return strings.Trim(fmt.Sprintf(`metric.type="%s" %s`, metricType, filterString), " ")
}

func buildSLOFilterExpression(q sloQuery) string {
	return fmt.Sprintf(`%s("projects/%s/services/%s/serviceLevelObjectives/%s")`, q.SelectorName, q.ProjectName, q.ServiceId, q.SloId)
}

func setMetricAggParams(params *url.Values, query *metricQuery, durationSeconds int, intervalMs int64) {
	if query.CrossSeriesReducer == "" {
		query.CrossSeriesReducer = crossSeriesReducerDefault
	}

	if query.PerSeriesAligner == "" {
		query.PerSeriesAligner = perSeriesAlignerDefault
	}

	alignmentPeriod := calculateAlignmentPeriod(query.AlignmentPeriod, intervalMs, durationSeconds)

	// In case a preprocessor is defined, the preprocessor becomes the primary aggregation
	// and the aggregation that is specified in the UI becomes the secondary aggregation
	// Rules are specified in this issue: https://github.com/grafana/grafana/issues/30866
	if query.PreprocessorType != PreprocessorTypeNone {
		params.Add("secondaryAggregation.alignmentPeriod", alignmentPeriod)
		params.Add("secondaryAggregation.crossSeriesReducer", query.CrossSeriesReducer)
		params.Add("secondaryAggregation.perSeriesAligner", query.PerSeriesAligner)

		primaryCrossSeriesReducer := crossSeriesReducerDefault
		if len(query.GroupBys) > 0 {
			primaryCrossSeriesReducer = query.CrossSeriesReducer
		}
		params.Add("aggregation.crossSeriesReducer", primaryCrossSeriesReducer)

		aligner := "ALIGN_RATE"
		if query.PreprocessorType == PreprocessorTypeDelta {
			aligner = "ALIGN_DELTA"
		}
		params.Add("aggregation.perSeriesAligner", aligner)

		for _, groupBy := range query.GroupBys {
			params.Add("secondaryAggregation.groupByFields", groupBy)
		}
	} else {
		params.Add("aggregation.crossSeriesReducer", query.CrossSeriesReducer)
		params.Add("aggregation.perSeriesAligner", query.PerSeriesAligner)
	}

	params.Add("aggregation.alignmentPeriod", alignmentPeriod)

	for _, groupBy := range query.GroupBys {
		params.Add("aggregation.groupByFields", groupBy)
	}
}

func setSloAggParams(params *url.Values, query *sloQuery, durationSeconds int, intervalMs int64) {
	params.Add("aggregation.alignmentPeriod", calculateAlignmentPeriod(query.AlignmentPeriod, intervalMs, durationSeconds))
	if query.SelectorName == "select_slo_health" {
		params.Add("aggregation.perSeriesAligner", "ALIGN_MEAN")
	} else {
		params.Add("aggregation.perSeriesAligner", "ALIGN_NEXT_OLDER")
	}
}

func calculateAlignmentPeriod(alignmentPeriod string, intervalMs int64, durationSeconds int) string {
	if alignmentPeriod == "grafana-auto" || alignmentPeriod == "" {
		alignmentPeriodValue := int(math.Max(float64(intervalMs)/1000, 60.0))
		alignmentPeriod = "+" + strconv.Itoa(alignmentPeriodValue) + "s"
	}

	if alignmentPeriod == "cloud-monitoring-auto" || alignmentPeriod == "stackdriver-auto" { // legacy
		alignmentPeriodValue := int(math.Max(float64(durationSeconds), 60.0))
		switch {
		case alignmentPeriodValue < 60*60*23:
			alignmentPeriod = "+60s"
		case alignmentPeriodValue < 60*60*24*6:
			alignmentPeriod = "+300s"
		default:
			alignmentPeriod = "+3600s"
		}
	}

	return alignmentPeriod
}

func formatLegendKeys(metricType string, defaultMetricName string, labels map[string]string,
	additionalLabels map[string]string, query *cloudMonitoringTimeSeriesFilter) string {
	if query.AliasBy == "" {
		return defaultMetricName
	}

	result := legendKeyFormat.ReplaceAllFunc([]byte(query.AliasBy), func(in []byte) []byte {
		metaPartName := strings.Replace(string(in), "{{", "", 1)
		metaPartName = strings.Replace(metaPartName, "}}", "", 1)
		metaPartName = strings.TrimSpace(metaPartName)

		if metaPartName == "metric.type" {
			return []byte(metricType)
		}

		metricPart := replaceWithMetricPart(metaPartName, metricType)

		if metricPart != nil {
			return metricPart
		}

		if val, exists := labels[metaPartName]; exists {
			return []byte(val)
		}

		if val, exists := additionalLabels[metaPartName]; exists {
			return []byte(val)
		}

		if metaPartName == "project" && query.ProjectName != "" {
			return []byte(query.ProjectName)
		}

		if metaPartName == "service" && query.Service != "" {
			return []byte(query.Service)
		}

		if metaPartName == "slo" && query.Slo != "" {
			return []byte(query.Slo)
		}

		if metaPartName == "selector" && query.Selector != "" {
			return []byte(query.Selector)
		}

		return in
	})

	return string(result)
}

func replaceWithMetricPart(metaPartName string, metricType string) []byte {
	// https://cloud.google.com/monitoring/api/v3/metrics-details#label_names
	shortMatches := metricNameFormat.FindStringSubmatch(metricType)

	if metaPartName == "metric.name" {
		if len(shortMatches) > 2 {
			return []byte(shortMatches[3])
		}
	}

	if metaPartName == "metric.service" {
		if len(shortMatches) > 0 {
			return []byte(shortMatches[1])
		}
	}

	return nil
}

func calcBucketBound(bucketOptions cloudMonitoringBucketOptions, n int) string {
	bucketBound := "0"
	if n == 0 {
		return bucketBound
	}

	switch {
	case bucketOptions.LinearBuckets != nil:
		bucketBound = strconv.FormatInt(bucketOptions.LinearBuckets.Offset+(bucketOptions.LinearBuckets.Width*int64(n-1)), 10)
	case bucketOptions.ExponentialBuckets != nil:
		bucketBound = strconv.FormatInt(int64(bucketOptions.ExponentialBuckets.Scale*math.Pow(bucketOptions.ExponentialBuckets.GrowthFactor, float64(n-1))), 10)
	case bucketOptions.ExplicitBuckets != nil:
		bucketBound = fmt.Sprintf("%g", bucketOptions.ExplicitBuckets.Bounds[n])
	}
	return bucketBound
}

func (s *Service) createRequest(ctx context.Context, pluginCtx backend.PluginContext, dsInfo *datasourceInfo, proxyPass string, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(dsInfo.url)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(u.Path, "render")

	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequest(method, cloudMonitoringRoute.url, body)
	if err != nil {
		slog.Error("Failed to create request", "error", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = proxyPass

	return req, nil
}

func (s *Service) getDefaultProject(ctx context.Context, dsInfo datasourceInfo) (string, error) {
	if dsInfo.authenticationType == gceAuthentication {
		defaultCredentials, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/monitoring.read")
		if err != nil {
			return "", fmt.Errorf("failed to retrieve default project from GCE metadata server: %w", err)
		}
		token, err := defaultCredentials.TokenSource.Token()
		if err != nil {
			return "", fmt.Errorf("failed to retrieve GCP credential token: %w", err)
		}
		if !token.Valid() {
			return "", errors.New("failed to validate GCP credentials")
		}

		return defaultCredentials.ProjectID, nil
	}
	return dsInfo.defaultProject, nil
}

func unmarshalResponse(res *http.Response) (cloudMonitoringResponse, error) {
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return cloudMonitoringResponse{}, err
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			slog.Warn("Failed to close response body", "err", err)
		}
	}()

	if res.StatusCode/100 != 2 {
		slog.Error("Request failed", "status", res.Status, "body", string(body))
		return cloudMonitoringResponse{}, fmt.Errorf("query failed: %s", string(body))
	}

	var data cloudMonitoringResponse
	err = json.Unmarshal(body, &data)
	if err != nil {
		slog.Error("Failed to unmarshal CloudMonitoring response", "error", err, "status", res.Status, "body", string(body))
		return cloudMonitoringResponse{}, fmt.Errorf("failed to unmarshal query response: %w", err)
	}

	return data, nil
}

func addConfigData(frames data.Frames, dl string, unit string) data.Frames {
	for i := range frames {
		if frames[i].Fields[1].Config == nil {
			frames[i].Fields[1].Config = &data.FieldConfig{}
		}
		deepLink := data.DataLink{
			Title:       "View in Metrics Explorer",
			TargetBlank: true,
			URL:         dl,
		}
		frames[i].Fields[1].Config.Links = append(frames[i].Fields[1].Config.Links, deepLink)
		if len(unit) > 0 {
			if val, ok := cloudMonitoringUnitMappings[unit]; ok {
				frames[i].Fields[1].Config.Unit = val
			}
		}
	}
	return frames
}

func (s *Service) getDSInfo(pluginCtx backend.PluginContext) (*datasourceInfo, error) {
	i, err := s.im.Get(pluginCtx)
	if err != nil {
		return nil, err
	}

	instance, ok := i.(*datasourceInfo)
	if !ok {
		return nil, fmt.Errorf("failed to cast datsource info")
	}

	return instance, nil
}
