package com.example.service;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class OrderService {
    
    @Transactional
    public void createOrder(String orderId) {
        // ... validation logic
        saveOrderInternal(orderId);
    }

    private void saveOrderInternal(String orderId) {
        // ... database logic
    }
}
