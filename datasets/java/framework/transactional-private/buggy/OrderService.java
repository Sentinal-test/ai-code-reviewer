package com.example.service;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class OrderService {
    
    public void createOrder(String orderId) {
        // ... validation logic
        saveOrderInternal(orderId);
    }

    // so no transaction is started. Also private methods cannot be proxied generally.
    @Transactional
    private void saveOrderInternal(String orderId) {
        // ... database logic
    }
}
