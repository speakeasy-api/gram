export function getServerUrlByKey(key: string): string {
    if (key.startsWith("local_")) {
        return "http://localhost:8080";
    } else if (key.startsWith("test_")) {
        return "https://dev.getgram.ai";
    } else if (key.startsWith("live_")) {
        return "https://getgram.ai";
    } else {
        return "https://getgram.ai";
    }
}