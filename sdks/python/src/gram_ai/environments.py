def get_server_url_by_key(key: str) -> str:
    if key.startswith("gram_local_"):
        return "http://localhost:8080"
    if key.startswith("gram_test_"):
        return "https://dev.getgram.ai"
    if key.startswith("gram_live_"):
        return "https://getgram.ai"
    return "https://getgram.ai"
