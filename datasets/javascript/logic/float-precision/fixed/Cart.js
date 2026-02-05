class Cart {
  constructor() {
    this.items = [];
    this.taxRate = 0.0825; // 8.25%
  }

  addItem(name, price, qty) {
    this.items.push({ name, price, qty });
  }

  calculateTotal() {
    // Better: work with cents (integers)
    let subtotalCents = 0;
    for (const item of this.items) {
      subtotalCents += (item.price * 100) * item.qty;
    }
    
    const taxCents = Math.round(subtotalCents * this.taxRate);
    return (subtotalCents + taxCents) / 100;
  }

  isEligibleForFreeShipping() {
    const total = this.calculateTotal();
    
    // FIXED: Use an epsilon for comparison or >= logic
    const EPSILON = 0.001;
    if (Math.abs(total - 50.00) < EPSILON) {
      return true;
    }
    return total > 50.00;
  }

  // Dummy methods
  clear() { this.items = []; }
  getCount() { return this.items.length; }
  removeItem(index) { this.items.splice(index, 1); }
  applyCoupon(code) { /* ... */ }
}

module.exports = Cart;
