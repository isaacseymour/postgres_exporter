package collector

import (
	"context"
	"time"

	"github.com/jackc/pgx"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// Subsystem
	statActivitySubsystem = "stat_activity"

	// Scrape query
	statActivityQuery = `
WITH states AS (
  SELECT datname
	   , unnest(array['active',
					  'idle',
					  'idle in transaction',
					  'idle in transaction (aborted)',
					  'fastpath function call',
					  'disabled']) AS state FROM pg_database
)
SELECT datname, state, COALESCE(count, 0) as count
  FROM states LEFT JOIN (
	   SELECT datname, state, count(*)::float
       FROM pg_stat_activity GROUP BY datname, state
	   ) AS activity
 USING (datname, state) /*postgres_exporter*/`

	// Oldest transaction timestamp
	// ignore when backend_xid is null, so excludes autovacuumn, autoanalyze
	// and other maintenance tasks
	statActivityCollectorXactQuery = `
SELECT EXTRACT(EPOCH FROM age(clock_timestamp(), coalesce(min(xact_start), current_timestamp))) AS xact_start
     , application_name
	 , datname
  FROM pg_stat_activity
 WHERE state IN ('idle in transaction', 'active')
   AND backend_xid IS NOT NULL
 GROUP BY application_name, datname /*postgres_exporter*/`

	// Oldest backend timestamp
	statActivityCollectorBackendStartQuery = `SELECT min(backend_start) FROM pg_stat_activity /*postgres_exporter*/`

	// Oldest query in running state (long queries)"
	statActivityCollectorActiveQuery = `
SELECT EXTRACT(EPOCH FROM age(clock_timestamp(), coalesce(min(query_start), clock_timestamp())))
     , application_name
     , datname
  FROM pg_stat_activity
 WHERE state='active'
 GROUP BY application_name, datname /*postgres_exporter*/`

	// Oldest Snapshot
	statActivityCollectorOldestSnapshotQuery = `
SELECT EXTRACT(EPOCH FROM age(clock_timestamp(), coalesce(min(query_start), clock_timestamp())))
     , application_name
     , datname
  FROM pg_stat_activity
 WHERE backend_xmin IS NOT NULL
 GROUP BY application_name, datname /*postgres_exporter*/`
)

type statActivityCollector struct {
	connections *prometheus.Desc
	backend     *prometheus.Desc
	xact        *prometheus.Desc
	active      *prometheus.Desc
	snapshot    *prometheus.Desc
}

func init() {
	registerCollector("stat_activity", defaultEnabled, NewStatActivityCollector)
}

// NewStatActivityCollector returns a new Collector exposing postgres pg_stat_activity
func NewStatActivityCollector() (Collector, error) {
	return &statActivityCollector{
		connections: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, statActivitySubsystem, "connections"),
			"Number of current connections in their current state",
			[]string{"datname", "state"},
			nil,
		),
		backend: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, statActivitySubsystem, "oldest_backend_timestamp"),
			"The oldest backend started timestamp",
			nil,
			nil,
		),
		xact: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, statActivitySubsystem, "oldest_xact_seconds"),
			"The oldest transaction (active or idle in transaction)",
			[]string{"application_name", "datname"},
			nil,
		),
		active: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, statActivitySubsystem, "oldest_query_active_seconds"),
			"The oldest query in running state (long query)",
			[]string{"application_name", "datname"},
			nil,
		),
		snapshot: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, statActivitySubsystem, "oldest_snapshot_seconds"),
			"The oldest query snapshot",
			[]string{"application_name", "datname"},
			nil,
		),
	}, nil
}

func (c *statActivityCollector) Update(ctx context.Context, db *pgx.Conn, ch chan<- prometheus.Metric) error {
	rows, err := db.QueryEx(ctx, statActivityQuery, nil)
	if err != nil {
		return err
	}

	var applicationName, datname, state string
	var count, oldestTx, oldestActive, oldestSnapshot float64
	var oldestBackend time.Time

	for rows.Next() {
		if err := rows.Scan(&datname, &state, &count); err != nil {
			return err
		}

		// postgres_stat_activity_connections
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, count, datname, state)
	}

	err = rows.Err()
	if err != nil {
		return err
	}
	rows.Close()

	err = db.QueryRowEx(ctx, statActivityCollectorBackendStartQuery, nil).Scan(&oldestBackend)
	if err != nil {
		return err
	}

	// postgres_stat_activity_oldest_backend_timestamp
	ch <- prometheus.MustNewConstMetric(c.backend, prometheus.GaugeValue, float64(oldestBackend.UTC().Unix()))

	if rows, err = db.QueryEx(ctx, statActivityCollectorXactQuery, nil); err != nil {
		return err
	}

	for rows.Next() {
		if err := rows.Scan(&oldestTx, &applicationName, &datname); err != nil {
			return err
		}

		// postgres_stat_activity_oldest_xact_seconds
		ch <- prometheus.MustNewConstMetric(c.xact, prometheus.GaugeValue, oldestTx, applicationName, datname)
	}

	err = rows.Err()
	if err != nil {
		return err
	}
	rows.Close()

	if rows, err = db.QueryEx(ctx, statActivityCollectorActiveQuery, nil); err != nil {
		return err
	}

	for rows.Next() {
		if err := rows.Scan(&oldestActive, &applicationName, &datname); err != nil {
			return err
		}

		// postgres_stat_activity_oldest_query_active_seconds
		ch <- prometheus.MustNewConstMetric(c.active, prometheus.GaugeValue, oldestActive, applicationName, datname)
	}

	err = rows.Err()
	if err != nil {
		return err
	}
	rows.Close()

	if rows, err = db.QueryEx(ctx, statActivityCollectorOldestSnapshotQuery, nil); err != nil {
		return err
	}

	for rows.Next() {
		if err := rows.Scan(&oldestSnapshot, &applicationName, &datname); err != nil {
			return err
		}

		// postgres_stat_activity_oldest_snapshot_seconds
		ch <- prometheus.MustNewConstMetric(c.snapshot, prometheus.GaugeValue, oldestSnapshot, applicationName, datname)

	}

	err = rows.Err()
	if err != nil {
		return err
	}
	rows.Close()

	return nil
}
