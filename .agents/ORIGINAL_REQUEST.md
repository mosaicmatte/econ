# Original User Request

## Initial Request — 2026-08-30T01:54:04Z

# Teamwork Project Prompt — Draft

> Status: Launched
> Goal: Fix UI bugs and add domestic home toggle
> Requested team: A small focused team

This is a single self-contained fix; keep it small and focused.
Fix several UI bugs on the dashboard and add a toggle to switch the 3D view between the 1-level domestic home model and the full multi-level building model.

Working directory: /Users/nguyenhoangkhoi/Documents/econ/dashboard
Integrity mode: development

## Requirements

### R1. Fix UI Rendering Bugs
- Fix the top tab bar where text is being cut off (e.g., "PLU..." instead of the full tab name).
- Fix the 3D visualization rendering bug where the blue connection lines/rays are misaligned, shooting off to incorrect points, or creating a chaotic web when zoomed/panned.
- Fix the issue where the entire visualization/screen appears darkened (e.g., stuck backdrop overlay, missing ambient lighting, or incorrect opacity settings).
- Fix the duplicated AI Load Forecast cards in the AI Insights panel. The "AI Load Forecast Trajectory" and "Load Forecast Not Yet Checked" cards currently display the exact same chart and data. Merge them or redesign the UI so they don't duplicate the chart and instead provide complementary, valuable information.

### R2. Add Domestic Home Toggle
- The codebase already contains the 3D asset for a 1-level domestic home.
- Add a UI toggle (e.g., a button or switch) below the 3D view that allows the user to switch between the 1-level domestic home model and the default full multi-level building model.
- Wire this toggle so that the selected 3D asset is correctly loaded and rendered in the viewer.

## Acceptance Criteria

### Automated UI Verification
- [ ] A Puppeteer/Playwright test script is provided or updated in the `dashboard` directory.
- [ ] The script automatically toggles the view to the domestic home model and verifies that the UI updates (e.g., by checking for a specific DOM element, canvas state, or log).
- [ ] The script verifies that the top tab bar renders without truncation.
