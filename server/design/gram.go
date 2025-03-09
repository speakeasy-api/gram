package design

import (
  . "goa.design/goa/v3/dsl"
)

// Service definition
var _ = Service("concerts", func() {
  Description("The concerts service manages music concert data.")

  Method("list", func() {
    Description("List upcoming concerts with optional pagination.")
    
    Payload(func() {
      Attribute("page", Int, "Page number", func() {
        Minimum(1)
        Default(1)
      })
      Attribute("limit", Int, "Items per page", func() {
        Minimum(1)
        Maximum(100)
        Default(10)
      })
    })

    Result(ArrayOf(Concert))

    HTTP(func() {
      GET("/concerts")

      // Query parameters for pagination
      Param("page", Int, "Page number", func() {
        Minimum(1)
      })
      Param("limit", Int, "Number of items per page", func() {
        Minimum(1)
        Maximum(100)
      })

      Response(StatusOK) // No need to specify the Body, it's inferred from the Result
    })
  })

  Method("create", func() {
    Description("Create a new concert entry.")
    
    Payload(ConcertPayload)
    Result(Concert)

    HTTP(func() {
      POST("/concerts")
      Response(StatusCreated)
    })
  })

  Method("show", func() {
    Description("Get a single concert by ID.")
    
    Payload(func() {
      Attribute("concertID", String, "Concert UUID", func() {
        Format(FormatUUID)
      })
      Required("concertID")
    })

    Result(Concert)
    Error("not_found")

    HTTP(func() {
      GET("/concerts/{concertID}")
      Response(StatusOK)
      Response("not_found", StatusNotFound)
    })
  })

  Method("update", func() {
    Description("Update an existing concert by ID.")

    Payload(func() {
      Extend(ConcertPayload)
      Attribute("concertID", String, "ID of the concert to update.", func() {
        Format(FormatUUID)
      })
      Required("concertID")
    })

    Result(Concert, "The updated concert.")

    Error("not_found", ErrorResult, "Concert not found")

    HTTP(func() {
      PUT("/concerts/{concertID}")

      Response(StatusOK)
      Response("not_found", StatusNotFound)
    })
  })

  Method("delete", func() {
    Description("Remove a concert from the system by ID.")

    Payload(func() {
      Attribute("concertID", String, "ID of the concert to remove.", func() {
        Format(FormatUUID)
      })
      Required("concertID")
    })

    Error("not_found", ErrorResult, "Concert not found")

    HTTP(func() {
      DELETE("/concerts/{concertID}")

      Response(StatusNoContent)
      Response("not_found", StatusNotFound)
    })
  })
})

// Data Types
var ConcertPayload = Type("ConcertPayload", func() {
  Description("Data needed to create/update a concert.")

  Attribute("artist", String, "Performing artist/band", func() {
    MinLength(1)
    Example("The Beatles")
  })
  Attribute("date", String, "Concert date (YYYY-MM-DD)", func() {
    Pattern(`^\d{4}-\d{2}-\d{2}$`)
    Example("2024-01-01")
  })
  Attribute("venue", String, "Concert venue", func() {
    MinLength(1)
    Example("The O2 Arena")
  })
  Attribute("price", Int, "Ticket price (USD)", func() {
    Minimum(1)
    Example(100)
  })
})

var Concert = Type("Concert", func() {
  Description("A concert with all its details.")
  Extend(ConcertPayload)
  
  Attribute("id", String, "Unique concert ID", func() {
    Format(FormatUUID)
  })
  Required("id", "artist", "date", "venue", "price")
})