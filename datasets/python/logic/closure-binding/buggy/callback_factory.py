from typing import List, Callable

class CallbackFactory:
    def __init__(self):
        self.callbacks = []

    def create_multipliers(self, factor: int) -> List[Callable]:
        """
        Creates a list of functions that multiply their input by 0..9.
        """
        multipliers = []
        
        # BUG: Late Binding Closures
        # In Python, the loop variable 'i' is captured by reference, not value.
        # By the time these lambdas execute, 'i' will be equal to the last value (9).
        # So all functions will multiply by 9.
        for i in range(10):
            multipliers.append(lambda x: x * i * factor)
            
        return multipliers

    def execute_all(self, value: int):
        funcs = self.create_multipliers(2)
        return [f(value) for f in funcs]
