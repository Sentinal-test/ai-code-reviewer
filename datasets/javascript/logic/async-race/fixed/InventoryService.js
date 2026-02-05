const { db } = require('../database');

class InventoryService {
  constructor() {
    this.table = 'inventory';
  }

  // FIXED: Atomic update
  async purchaseItem(itemId, quantity) {
    // Use database atomic decrement or transaction with SELECT FOR UPDATE
    const result = await db.query(
      'UPDATE inventory SET stock = stock - ? WHERE id = ? AND stock >= ? RETURNING stock',
      [quantity, itemId, quantity]
    );

    if (result.affectedRows === 0) {
      throw new Error('Out of stock or invalid item');
    }

    return { status: 'purchased', remaining: result.rows[0].stock };
  }

  async restockItem(itemId, quantity) {
    // Atomic increment
    await db.query('UPDATE inventory SET stock = stock + ? WHERE id = ?', [quantity, itemId]);
    return { status: 'restocked' };
  }
}

module.exports = InventoryService;
