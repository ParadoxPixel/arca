package store

import (
	"arca/internal/store/models"
)

type TrafficBucket struct {
	At        int64 `json:"at"`
	Downloads int64 `json:"downloads"`
	Uploads   int64 `json:"uploads"`
	Visits    int64 `json:"visits"`
	Bytes     int64 `json:"bytes"`
}

type TrafficTotals struct {
	Downloads int64 `json:"downloads"`
	Uploads   int64 `json:"uploads"`
	Visits    int64 `json:"visits"`
	Bytes     int64 `json:"bytes"`
}

type ArtifactTraffic struct {
	Repository string `json:"repository"`
	Format     string `json:"format"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	Downloads  int64  `json:"downloads"`
	Bytes      int64  `json:"bytes"`
	LastAt     int64  `json:"lastAt"`
}

type RepositoryTraffic struct {
	Repository string `json:"repository"`
	Downloads  int64  `json:"downloads"`
	Uploads    int64  `json:"uploads"`
	Bytes      int64  `json:"bytes"`
}

const countsByKind = `
	SUM(kind = '` + models.TrafficDownload + `') AS downloads,
	SUM(kind = '` + models.TrafficUpload + `') AS uploads,
	SUM(kind = '` + models.TrafficVisit + `') AS visits,
	COALESCE(SUM(bytes), 0) AS bytes`

func (s *Store) RecordTraffic(events []models.TrafficEvent) error {
	if len(events) == 0 {
		return nil
	}
	for index := range events {
		events[index].ID = NewID()
	}
	return s.db.Create(&events).Error
}

func (s *Store) DeleteTrafficBefore(cutoff int64) (int64, error) {
	result := s.db.Where("at < ?", cutoff).Delete(&models.TrafficEvent{})
	return result.RowsAffected, result.Error
}

func (s *Store) TrafficSeries(since, until, interval int64) ([]TrafficBucket, error) {
	type row struct {
		Bucket    int64
		Downloads int64
		Uploads   int64
		Visits    int64
		Bytes     int64
	}

	rows := []row{}
	err := s.db.Raw(
		`SELECT (at - ?) / ? AS bucket,`+countsByKind+`
		 FROM traffic_events WHERE at >= ? AND at < ?
		 GROUP BY bucket ORDER BY bucket`,
		since, interval, since, until,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	filled := map[int64]row{}
	for _, entry := range rows {
		filled[entry.Bucket] = entry
	}

	buckets := make([]TrafficBucket, 0, (until-since)/interval)
	for index := int64(0); since+index*interval < until; index++ {
		entry := filled[index]
		buckets = append(buckets, TrafficBucket{
			At:        since + index*interval,
			Downloads: entry.Downloads,
			Uploads:   entry.Uploads,
			Visits:    entry.Visits,
			Bytes:     entry.Bytes,
		})
	}
	return buckets, nil
}

func (s *Store) TrafficTotals(since, until int64) (TrafficTotals, error) {
	var totals TrafficTotals
	err := s.db.Raw(
		`SELECT`+countsByKind+` FROM traffic_events WHERE at >= ? AND at < ?`,
		since, until,
	).Scan(&totals).Error
	return totals, err
}

func (s *Store) TopArtifacts(since, until int64, limit int) ([]ArtifactTraffic, error) {
	artifacts := []ArtifactTraffic{}
	err := s.db.Raw(
		`SELECT repositories.name AS repository, repositories.format, traffic_events.namespace, traffic_events.name,
		        COUNT(*) AS downloads, COALESCE(SUM(traffic_events.bytes), 0) AS bytes, MAX(traffic_events.at) AS last_at
		 FROM traffic_events
		 JOIN repositories ON repositories.id = traffic_events.repository_id
		 WHERE traffic_events.kind = ? AND traffic_events.at >= ? AND traffic_events.at < ?
		   AND traffic_events.name != ''
		 GROUP BY repository, repositories.format, traffic_events.namespace, traffic_events.name
		 ORDER BY downloads DESC, bytes DESC
		 LIMIT ?`,
		models.TrafficDownload, since, until, limit,
	).Scan(&artifacts).Error
	return artifacts, err
}

func (s *Store) TrafficByRepository(since, until int64) ([]RepositoryTraffic, error) {
	repositories := []RepositoryTraffic{}
	err := s.db.Raw(
		`SELECT repositories.name AS repository,
		        SUM(traffic_events.kind = ?) AS downloads,
		        SUM(traffic_events.kind = ?) AS uploads,
		        COALESCE(SUM(traffic_events.bytes), 0) AS bytes
		 FROM traffic_events
		 JOIN repositories ON repositories.id = traffic_events.repository_id
		 WHERE traffic_events.at >= ? AND traffic_events.at < ?
		 GROUP BY repository
		 ORDER BY downloads DESC, bytes DESC`,
		models.TrafficDownload, models.TrafficUpload, since, until,
	).Scan(&repositories).Error
	return repositories, err
}
