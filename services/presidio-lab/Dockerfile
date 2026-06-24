# syntax=docker/dockerfile:1
FROM mcr.microsoft.com/presidio-analyzer:2.2.362

ENV PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN pip install --no-cache-dir \
    fastapi==0.115.6 \
    uvicorn[standard]==0.32.1

WORKDIR /app
COPY app.py /app/app.py

EXPOSE 3000

CMD ["python", "-m", "uvicorn", "app:app", "--host", "0.0.0.0", "--port", "3000", "--workers", "1"]
