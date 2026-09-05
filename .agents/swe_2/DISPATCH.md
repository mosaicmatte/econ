# DISPATCH — 2026-08-30T01:10:59Z

## 2026-08-30T01:10:59Z
<USER_REQUEST>
You are the SWE Light Orchestrator for this task.

Working Directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/swe_2
Workspace Root: /Users/nguyenhoangkhoi/Documents/econ
Dashboard Directory: /Users/nguyenhoangkhoi/Documents/econ/dashboard
Authoritative Request: /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md

Your Task:
Execute the SWE Light lifecycle (one implementer on the whole task, then reviewer rounds with cumulative open-issues ledger and automated test verification) for the following requirements:

1. Fix UI Rendering Bugs:
   - Fix the top tab bar where text is being cut off (e.g., "PLU..." instead of the full tab name).
   - Fix the 3D visualization rendering bug where the blue connection lines/rays are misaligned, shooting off to incorrect points, or creating a chaotic web when zoomed/panned.
   - Fix the issue where the entire visualization/screen appears darkened (e.g., stuck backdrop overlay, missing ambient lighting, or incorrect opacity settings).
   - Fix the duplicated AI Load Forecast cards in the AI Insights panel ("AI Load Forecast Trajectory" and "Load Forecast Not Yet Checked" displaying the exact same chart and data; merge or redesign UI so they don't duplicate and provide complementary valuable information).

2. Add Domestic Home Toggle:
   - Use the existing 3D asset for the 1-level domestic home already in the codebase.
   - Add a UI toggle (e.g., button/switch) below the 3D view to switch between the 1-level domestic home model and default multi-level building model.
   - Wire this toggle so the selected 3D asset is correctly loaded and rendered in the viewer.

3. Automated UI Verification:
   - Provide or update a Puppeteer/Playwright test script in the dashboard directory.
   - Verify view toggling to domestic home model and UI updates, plus verify top tab bar renders without truncation.

Coordinate your implementer and reviewer subagents. Write all your coordination files under /Users/nguyenhoangkhoi/Documents/econ/.agents/swe_2/. When complete, report completion and handoff.md back to me.
</USER_REQUEST>
