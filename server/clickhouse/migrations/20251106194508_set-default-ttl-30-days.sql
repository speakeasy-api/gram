ALTER TABLE `http_requests_raw` MODIFY TTL ts + toIntervalDay(30);
