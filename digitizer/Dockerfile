# ECON digitizer — turns a real-world blueprint (DXF / PDF / image / photo) into
# building-data.json using the Branch B pipeline. Build context is econ/ so the
# service can import ai_modules/branch_b_digitization directly instead of forking it.
FROM python:3.11-slim

# poppler renders PDF blueprints to raster pages for the CV pipeline.
RUN apt-get update && apt-get install -y --no-install-recommends poppler-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY digitizer/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY ai_modules/branch_b_digitization /app/branch_b
COPY digitizer/app.py .

ENV PYTHONPATH=/app/branch_b
EXPOSE 8000
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
