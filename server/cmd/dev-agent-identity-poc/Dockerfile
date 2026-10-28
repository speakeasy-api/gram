FROM python:3.13.12-slim-bookworm@sha256:a58daefb915e1e03ad48f3ca4df8832065412c5c35cacb9d39f4229184de12b6
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PIP_DISABLE_PIP_VERSION_CHECK=1
WORKDIR /opt/probe
COPY requirements.txt ./
RUN python3 -m pip install --no-cache-dir --require-hashes --only-binary=:all: -r requirements.txt
COPY request_token.py test_request_token.py ./
RUN python3 -m unittest -v test_request_token
USER 1000:1000
CMD ["/bin/sh", "-c", "sleep 3600"]
