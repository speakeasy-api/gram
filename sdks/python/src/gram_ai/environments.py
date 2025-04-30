def get_server_url_by_key(key: str) -> str:
    if key.startswith("local_"):
        return "http://localhost:8080"
    if key.startswith("test_"):
        return "https://dev.getgram.ai"
    if key.startswith("live_"):
        return "https://getgram.ai"
    return "https://getgram.ai"
