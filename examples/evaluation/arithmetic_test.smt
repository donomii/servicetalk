"A receipt, FizzBuzz, independent closures, and a loop."
check := EvaluationChecks new.
bill := 12.50.
tip := 0.2.
check equal: (bill + (bill * tip)) to: 15 named: 'receipt total'.
fizzbuzz := (1 to: 15) collect: [:n |
    (n \\ 15 = 0) ifTrue: ['FizzBuzz'] ifFalse: [
        (n \\ 3 = 0) ifTrue: ['Fizz'] ifFalse: [
            (n \\ 5 = 0) ifTrue: ['Buzz'] ifFalse: [n asString]
        ]
    ]
].
check equal: (fizzbuzz join: ',') to: '1,2,Fizz,4,Buzz,Fizz,7,8,Fizz,Buzz,11,Fizz,13,14,FizzBuzz' named: 'FizzBuzz'.
adder := [:x | [:y | x + y]].
addTwo := adder value: 2.
addThree := adder value: 3.
check equal: (addTwo value: 10) to: 12 named: 'first closure'.
check equal: (addThree value: 10) to: 13 named: 'second closure'.
factorial := 1.
1 to: 6 do: [:n | factorial := factorial * n].
check equal: factorial to: 720 named: 'loop factorial'.
Transcript show: 'receipt, FizzBuzz, closures and loop passed'.
fizzbuzz join: ','.
