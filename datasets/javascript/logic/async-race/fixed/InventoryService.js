const { db } = require('../database');

class InventoryService {
  constructor() {
    this.table = 'inventory';
  }

  async purchaseItem(itemId, quantity) {
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
    await db.query('UPDATE inventory SET stock = stock + ? WHERE id = ?', [quantity, itemId]);
    return { status: 'restocked' };
  }
}

module.exports = InventoryService;
