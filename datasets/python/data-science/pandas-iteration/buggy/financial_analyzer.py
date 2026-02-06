import pandas as pd
import numpy as np

class FinancialAnalyzer:
    def __init__(self, data: pd.DataFrame):
        self.df = data

    def calculate_growth(self):
        """
        Calculates week-over-week growth for each row.
        """
        self.df['growth'] = 0.0
        
        for index, row in self.df.iterrows():
            if row['previous_value'] > 0:
                growth = (row['current_value'] - row['previous_value']) / row['previous_value']
                self.df.at[index, 'growth'] = growth
        
        return self.df

    def normalize_data(self):
        # Using apply is better than iterrows but still slower than vectorization
        self.df['normalized'] = self.df.apply(lambda x: x['current_value'] / 100, axis=1)

    def summary(self):
        return self.df.describe()
