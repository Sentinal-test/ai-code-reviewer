package com.example.service;

import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

public class CacheService {
    // FIXED: Use ConcurrentHashMap for thread safety
    private final Map<String, Object> cache = new ConcurrentHashMap<>();

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
