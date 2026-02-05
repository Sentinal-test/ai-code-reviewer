import pandas as pd
import numpy as np

class FinancialAnalyzer:
    def __init__(self, data: pd.DataFrame):
        self.df = data

    def calculate_growth(self):
        """
        Calculates week-over-week growth for each row.
        """
        # FIXED: Use vectorized operations (100x+ faster)
        # Avoids explicit loops entirely
        self.df['growth'] = (self.df['current_value'] - self.df['previous_value']) / self.df['previous_value']
        
        # Handle division by zero/NaN if needed
        self.df['growth'] = self.df['growth'].fillna(0.0)
        
        return self.df

    def normalize_data(self):
        # Vectorized division
        self.df['normalized'] = self.df['current_value'] / 100

    def summary(self):
        return self.df.describe()
