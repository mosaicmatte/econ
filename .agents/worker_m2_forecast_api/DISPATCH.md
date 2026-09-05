## 2026-08-29T20:59:45Z
You are Worker M2 (End-to-End Forecast API Integration).
Your working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2_forecast_api
You MUST read /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md and /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md before starting work.

DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Your task:
1. Extend `RecommendationReport` in `server/simulation/recommend.go` to include `Forecast` / `ForecastGraph` structure (series, upper band, quantiles, peak load, step minutes, horizon, engine, plausibility).
2. Wire `recommendationsHandler` in `server/recommendapi.go` and/or `server/forecast.go` to attach the forecast graph data to `GET /api/recommendations`.
3. Create automated integration test in `server/recommendapi_test.go` asserting that `GET /api/recommendations` returns the forecast graph data with non-empty fields/series.
4. Run `go test -v ./...` in `server/` and verify that all Go tests pass.
5. Write your handoff report to `/Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2_forecast_api/handoff.md` and send a message.
