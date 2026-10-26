"Stored and computed properties, both edited through ordinary setter methods."
class Thermometer [
    | temperature |
    initialize [ temperature := 20 ]
    celsius [ ^ temperature ]
    celsius: degrees [
        (degrees < -273.15) ifTrue: [
            'Temperature cannot be below absolute zero' signal
        ] ifFalse: [
            temperature := degrees
        ].
        ^ temperature
    ]
    fahrenheit [ ^ (temperature * 9 / 5) + 32 ]
    fahrenheit: degrees [ ^ self celsius: ((degrees - 32) * 5 / 9) ]
    inspectorViews [ ^ '{"views":[{"id":"temperature","title":"Temperature","body":{"kind":"column","children":[{"kind":"row","children":[{"kind":"value","label":"Celsius","getter":"celsius"},{"kind":"value","label":"Fahrenheit","getter":"fahrenheit"}]},{"kind":"slider","label":"Celsius control","getter":"celsius","setter":"celsius:","type":"number","min":-40,"max":100,"step":1},{"kind":"input","label":"Fahrenheit input","getter":"fahrenheit","setter":"fahrenheit:","type":"number"}]}},{"id":"readings","title":"Readings","body":{"kind":"table","getter":"readings","columns":[{"key":"unit","label":"Unit"},{"key":"value","label":"Reading"}]}}]}' ]
    readings [ ^ '[{"unit":"Celsius","value":' , self celsius asString , '},{"unit":"Fahrenheit","value":' , self fahrenheit asString , '}]' ]
    inspectorMetadata [
        ^ '{"methods":{"celsius:":{"arguments":[{"name":"degrees","type":"number"}],"returns":"number"},"fahrenheit:":{"arguments":[{"name":"degrees","type":"number"}],"returns":"number"}},"properties":[{"name":"celsius","getter":"celsius","setter":"celsius:","type":"number"},{"name":"fahrenheit","getter":"fahrenheit","setter":"fahrenheit:","type":"number"}]}'
    ]
]
