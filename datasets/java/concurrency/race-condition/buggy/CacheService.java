package com.example.service;

import java.util.HashMap;
import java.util.Map;

public class CacheService {
    // or data corruption (lost updates).
    private final Map<String, Object> cache = new HashMap<>();

    public void put(String key, Object value) {
        cache.put(key, value);
    }

    public Object get(String key) {
        return cache.get(key);
    }

    public void clear() {
        cache.clear();
    }
}
