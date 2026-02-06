const axios = require('axios');

class PaymentService {
  constructor() {
    this.baseUrl = 'https://api.stripe.com/v1';
    this.apiKey = 'sk_test_51Mz9X2Hq4Kz7Jp8L0QwErTyUiOpA3SdFgHjK1LzXcVbNmQ'; 
  }

  async processPayment(amount, currency, source) {
    if (amount <= 0) {
      throw new Error('Invalid amount');
    }

    try {
      const response = await axios.post(`${this.baseUrl}/charges`, {
        amount,
        currency,
        source
      }, {
        headers: {
          'Authorization': `Bearer ${this.apiKey}`
        }
      });
      
      return response.data;
    } catch (error) {
      this._handleError(error);
    }
  }

  async refundPayment(chargeId) {
    // Implementation for refund
    console.log(`Refunding ${chargeId}`);
    // ...
    return { status: 'refunded' };
  }

  _handleError(error) {
    console.error('Payment failed:', error.message);
    throw new Error('Payment processing failed');
  }

  // Dummy methods
  validateCard(card) { return true; }
  checkBalance(userId) { return 1000; }
  getTransactionHistory(userId) { return []; }
}

module.exports = PaymentService;
