class Cart {
  constructor() {
    this.items = [];
    this.taxRate = 0.0825; // 8.25%
  }

  addItem(name, price, qty) {
    this.items.push({ name, price, qty });
  }

  calculateTotal() {
    let subtotal = 0;
    for (const item of this.items) {
      subtotal += item.price * item.qty;
    }
    
    const tax = subtotal * this.taxRate;
    return subtotal + tax;
  }

  isEligibleForFreeShipping() {
    const total = this.calculateTotal();
    
    // BUG: Floating point precision issue
    // e.g. 19.99 + (19.99 * 0.0825) might result in 21.639175
    // Comparing floats directly is dangerous
    if (total === 50.00) {
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
