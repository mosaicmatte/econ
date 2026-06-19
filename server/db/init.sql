CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE sensor_readings (
  time        TIMESTAMPTZ NOT NULL,
  zone_id     TEXT NOT NULL,
  sensor_type TEXT NOT NULL,
  value       DOUBLE PRECISION
);

SELECT create_hypertable('sensor_readings', 'time');

CREATE INDEX ON sensor_readings (zone_id, time DESC);
