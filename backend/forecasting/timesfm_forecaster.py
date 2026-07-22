"""Google TimesFM — zero-shot load forecasting for the twin.

Why this sits alongside the LSTM rather than replacing it outright:

The LSTM in model.py is a supervised model. It only knows this building after train.py has
been run against enough accumulated history, and until then it is either untrained (and
honestly refuses to serve) or trained on synthetic data. That is a real cold-start problem
for a twin that is supposed to be useful on day one.

TimesFM is a pretrained decoder-only foundation model for time series. It forecasts a
series it has never seen, with no training, no fitted scaler and no labels — you hand it
the building's recent load history and it returns the next H steps. So the twin can produce
a genuine load forecast from its first day of telemetry, and the LSTM (once it has real
history to learn from, and can specialise on THIS building) remains the model to graduate
to. The two answer the same question with opposite trade-offs, and /model/info reports
which one actually served a given forecast.

Model support, in preference order:
  1. google/timesfm-2.5-200m-transformers  — newer AND smaller (231M). Needs a transformers
     build that ships `TimesFm2_5ModelForPrediction` (the timesfm_2p5 architecture).
  2. google/timesfm-2.0-500m-pytorch       — 500M, runs on the long-standing
     `TimesFmModelForPrediction` class available in older transformers.

Everything here degrades rather than fails: no torch, no transformers, no checkpoint, or no
network all leave `ready == False` and a reason string, and the service keeps serving the
LSTM path. Nothing about this module is required for the twin to run.
"""
from __future__ import annotations

import os
import threading

# TimesFM is an OPTIONAL dependency. Importing torch/transformers at module scope would
# make the whole forecasting service unstartable on a machine that only wants the LSTM.
try:
    import torch
    _TORCH_ERR = None
except Exception as e:  # pragma: no cover - environment dependent
    torch = None
    _TORCH_ERR = str(e)


# Candidate (checkpoint, transformers class name) pairs, best first.
TIMESFM_CANDIDATES = [
    ("google/timesfm-2.5-200m-transformers", "TimesFm2_5ModelForPrediction", "2.5-200m"),
    ("google/timesfm-2.0-500m-pytorch", "TimesFmModelForPrediction", "2.0-500m"),
]

# Environment overrides, so an operator can pin a checkpoint or force a device.
ENV_MODEL = "TIMESFM_MODEL"        # explicit repo id
ENV_DEVICE = "TIMESFM_DEVICE"      # "cpu" | "mps" | "cuda"
ENV_ENABLE = "TIMESFM_ENABLED"     # "0" disables entirely


def pick_device(preferred: str | None = None) -> str:
    """Choose the accelerator this machine can actually use.

    This is the server-side twin of the client-side hardware matching in
    server/modelcatalog.go: the same reasoning about what a given machine can run, applied
    to the process that will do the running.
    """
    if torch is None:
        return "cpu"
    if preferred in ("cpu", "mps", "cuda"):
        return preferred
    if torch.cuda.is_available():
        return "cuda"
    if getattr(torch.backends, "mps", None) is not None and torch.backends.mps.is_available():
        return "mps"
    return "cpu"


class TimesFmForecaster:
    """Lazy, thread-safe holder for a TimesFM checkpoint.

    Loading is deferred until the first forecast is actually requested: the checkpoint is
    ~1-2 GB, and a service that only ever serves the LSTM should never pay for it.
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._model = None
        self._loaded = False
        self.variant: str | None = None
        self.repo_id: str | None = None
        self.device: str = "cpu"
        self.reason: str | None = None  # why it is unavailable, when it is

    # -- availability ------------------------------------------------------

    @property
    def ready(self) -> bool:
        return self._model is not None

    def disabled_reason(self) -> str | None:
        if os.getenv(ENV_ENABLE, "1") == "0":
            return f"disabled by {ENV_ENABLE}=0"
        if torch is None:
            return f"torch not installed ({_TORCH_ERR})"
        return None

    def _resolve_class(self):
        """Find the best (repo, class) pair this transformers build can actually load.

        Returns (repo_id, cls, variant) or (None, None, None) with self.reason set."""
        try:
            import transformers
        except Exception as e:
            self.reason = f"transformers not installed ({e})"
            return None, None, None

        forced = os.getenv(ENV_MODEL)
        candidates = TIMESFM_CANDIDATES
        if forced:
            # An explicit pin still has to find a class that can load it; try both.
            candidates = [(forced, c, f"pinned:{forced}") for _, c, _ in TIMESFM_CANDIDATES]

        missing = []
        for repo, cls_name, variant in candidates:
            cls = getattr(transformers, cls_name, None)
            if cls is not None:
                return repo, cls, variant
            missing.append(cls_name)
        self.reason = (
            f"installed transformers {getattr(transformers, '__version__', '?')} has none of "
            f"{missing}. TimesFM 2.5 needs a build shipping TimesFm2_5ModelForPrediction; "
            f"upgrade transformers, or pin an older checkpoint via {ENV_MODEL}."
        )
        return None, None, None

    def load(self) -> bool:
        """Load the checkpoint once. Returns True if a model is ready."""
        if self._loaded:
            return self.ready
        with self._lock:
            if self._loaded:
                return self.ready
            self._loaded = True

            reason = self.disabled_reason()
            if reason:
                self.reason = reason
                return False

            repo, cls, variant = self._resolve_class()
            if cls is None:
                return False

            self.device = pick_device(os.getenv(ENV_DEVICE))
            try:
                model = cls.from_pretrained(repo)
                # float32 everywhere: MPS has incomplete float64 support, and these models
                # are small enough that fp32 costs little.
                model = model.to(torch.float32)
                try:
                    model = model.to(self.device)
                except Exception as e:
                    # A device that torch advertises but cannot actually host the model is
                    # a real failure mode; fall back rather than dying.
                    print(f"[timesfm] {self.device} unusable ({e}); falling back to CPU")
                    self.device = "cpu"
                    model = model.to("cpu")
                model.eval()
            except Exception as e:
                self.reason = f"could not load {repo}: {e}"
                return False

            self._model = model
            self.repo_id = repo
            self.variant = variant
            self.reason = None
            print(f"[timesfm] loaded {repo} ({variant}) on {self.device}")
            return True

    # -- inference ---------------------------------------------------------

    def forecast(self, history: list[float], horizon: int = 12,
                 context_len: int | None = None) -> dict:
        """Forecast the next `horizon` steps of a single series, zero-shot.

        `history` is the building's own recent load in MW, oldest first. Returns the point
        forecast plus, when the checkpoint provides them, quantiles — a forecast that
        reports its own spread is far more useful for a pre-cool decision than a bare
        number, because the decision is really about the risk of the peak, not its mean.
        """
        if not self.load():
            raise RuntimeError(self.reason or "TimesFM unavailable")
        if not history:
            raise ValueError("history must be non-empty")

        series = torch.tensor([float(v) for v in history], dtype=torch.float32)
        kwargs = {"past_values": [series.to(self.device)]}
        if context_len:
            kwargs["forecast_context_len"] = int(context_len)
        # NOTE: the model exposes a `truncate_negative` flag that would clamp the forecast
        # to non-negative values in-graph, which is what building load needs. We do NOT use
        # it: in transformers 4.57.x that path calls torch.maximum(tensor, 0.0) with a
        # Python float and raises TypeError. Clamping the decoded output ourselves (below)
        # is equivalent, and portable across transformers versions.

        with torch.no_grad():
            out = self._model(**kwargs)

        mean = out.mean_predictions[0].detach().float().cpu().tolist()
        point = [max(0.0, v) for v in mean[:horizon]]

        quantiles = None
        full = getattr(out, "full_predictions", None)
        if full is not None:
            try:
                # full_predictions is (batch, horizon, quantiles+1); column 0 mirrors the
                # mean and the rest are the quantile heads.
                arr = full[0].detach().float().cpu().tolist()
                cols = list(zip(*arr))  # -> per-quantile series
                if len(cols) > 1:
                    quantiles = {
                        f"q{i}": [max(0.0, v) for v in col[:horizon]]
                        for i, col in enumerate(cols[1:], start=1)
                    }
            except Exception:
                quantiles = None

        return {
            "forecast": point,
            "quantiles": quantiles,
            "engine": "timesfm",
            "variant": self.variant,
            "repo": self.repo_id,
            "device": self.device,
            "zero_shot": True,
            "context_used": len(history),
        }

    def info(self) -> dict:
        """Availability without forcing a multi-gigabyte load — the honest readout for
        /model/info and for the dashboard's model card."""
        reason = self.disabled_reason()
        if reason and not self.ready:
            return {"available": False, "loaded": False, "reason": reason}
        if self.ready:
            return {
                "available": True, "loaded": True, "variant": self.variant,
                "repo": self.repo_id, "device": self.device, "zero_shot": True,
            }
        # Not loaded yet: report what WOULD be used, without downloading anything.
        probe = TimesFmForecaster()
        repo, cls, variant = probe._resolve_class()
        if cls is None:
            return {"available": False, "loaded": False, "reason": probe.reason}
        return {
            "available": True, "loaded": False, "variant": variant, "repo": repo,
            "device": pick_device(os.getenv(ENV_DEVICE)), "zero_shot": True,
            "note": "checkpoint downloads on first forecast",
        }


# One process-wide instance; loading is lazy so importing this costs nothing.
TIMESFM = TimesFmForecaster()
