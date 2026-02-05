const { db } = require('../database');

class InventoryService {
  constructor() {
    this.table = 'inventory';
  }

  // Two concurrent requests can read the same stock value,
  // decrement it, and write it back, resulting in overselling.
  async purchaseItem(itemId, quantity) {
    const item = await db.query('SELECT stock FROM inventory WHERE id = ?', [itemId]);
    
    if (!item || item.stock < quantity) {
      throw new Error('Out of stock');
    }

    // Simulate processing delay
    await new Promise(resolve => setTimeout(resolve, 100));

    const newStock = item.stock - quantity;
    await db.query('UPDATE inventory SET stock = ? WHERE id = ?', [newStock, itemId]);
    
    return { status: 'purchased', remaining: newStock };
  }

  async restockItem(itemId, quantity) {
    const item = await db.query('SELECT stock FROM inventory WHERE id = ?', [itemId]);
    const newStock = item.stock + quantity;
    await db.query('UPDATE inventory SET stock = ? WHERE id = ?', [newStock, itemId]);
    return { status: 'restocked', newStock };
  }
}

module.exports = InventoryService;
